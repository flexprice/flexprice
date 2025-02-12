package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api"
	"github.com/flexprice/flexprice/internal/api/cron"
	v1 "github.com/flexprice/flexprice/internal/api/v1"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/dynamodb"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook"
	"go.uber.org/fx"

	lambdaEvents "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
	_ "github.com/flexprice/flexprice/docs/swagger"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/gin-gonic/gin"
)

// @title FlexPrice API
// @version 1.0
// @description FlexPrice API Service
// @BasePath /v1
// @schemes http https
// @securityDefinitions.apikey ApiKeyAuth
// @in header
// @name x-api-key
// @description Enter your API key in the format *x-api-key &lt;api-key&gt;**

func init() {
	// Set UTC timezone for the entire application
	time.Local = time.UTC
}

func main() {
	// Initialize Fx application
	var opts []fx.Option

	// Core dependencies
	opts = append(opts,
		fx.Provide(
			// Config
			config.NewConfig,

			// Logger
			logger.NewLogger,

			// Monitoring
			sentry.NewSentryService,

			// DB
			postgres.NewDB,
			postgres.NewEntClient,
			postgres.NewClient,
			clickhouse.NewClickHouseStore,

			// Optional DBs
			dynamodb.NewClient,

			// Producers and Consumers
			kafka.NewProducer,
			kafka.NewConsumer,

			// Event Publisher
			publisher.NewEventPublisher,

			// Repositories
			repository.NewEventRepository,
			repository.NewMeterRepository,
			repository.NewUserRepository,
			repository.NewAuthRepository,
			repository.NewPriceRepository,
			repository.NewCustomerRepository,
			repository.NewPlanRepository,
			repository.NewSubscriptionRepository,
			repository.NewWalletRepository,
			repository.NewTenantRepository,
			repository.NewEnvironmentRepository,
			repository.NewInvoiceRepository,
			repository.NewFeatureRepository,
			repository.NewEntitlementRepository,

			// PubSub
			pubsubRouter.NewRouter,
		),
	)

	// Webhook module (must be initialised before services)
	opts = append(opts, webhook.Module)

	// Service layer
	opts = append(opts,
		fx.Provide(
			// Core services
			service.NewTenantService,
			service.NewAuthService,
			service.NewUserService,

			// Business services
			service.NewMeterService,
			service.NewEventService,
			service.NewPriceService,
			service.NewCustomerService,
			service.NewPlanService,
			service.NewSubscriptionService,
			service.NewWalletService,
			service.NewInvoiceService,
			service.NewFeatureService,
			service.NewEntitlementService,
		),
	)

	// API and Temporal
	opts = append(opts,
		fx.Provide(
			provideHandlers,
			provideRouter,
			provideTemporalConfig,
			temporal.NewTemporalClient,
			provideTemporalService,
		),
		fx.Invoke(
			sentry.RegisterHooks,
			startServer, // ✅ Ensure it's called only once here
		),
	)

	app := fx.New(opts...)
	app.Run()
}

func provideHandlers(
	cfg *config.Configuration,
	logger *logger.Logger,
	meterService service.MeterService,
	eventService service.EventService,
	authService service.AuthService,
	userService service.UserService,
	priceService service.PriceService,
	customerService service.CustomerService,
	planService service.PlanService,
	subscriptionService service.SubscriptionService,
	walletService service.WalletService,
	tenantService service.TenantService,
	invoiceService service.InvoiceService,
	temporalService *service.TemporalService,
	featureService service.FeatureService,
	entitlementService service.EntitlementService,
) api.Handlers {
	return api.Handlers{
		Events:       v1.NewEventsHandler(eventService, logger),
		Meter:        v1.NewMeterHandler(meterService, logger),
		Auth:         v1.NewAuthHandler(cfg, authService, logger),
		User:         v1.NewUserHandler(userService, logger),
		Health:       v1.NewHealthHandler(logger),
		Price:        v1.NewPriceHandler(priceService, logger),
		Customer:     v1.NewCustomerHandler(customerService, logger),
		Plan:         v1.NewPlanHandler(planService, entitlementService, logger),
		Subscription: v1.NewSubscriptionHandler(subscriptionService, logger),
		Wallet:       v1.NewWalletHandler(walletService, logger),
		Tenant:       v1.NewTenantHandler(tenantService, logger),
		Cron:         cron.NewSubscriptionHandler(subscriptionService, logger),
		Invoice:      v1.NewInvoiceHandler(invoiceService, logger),
		Feature:      v1.NewFeatureHandler(featureService, logger),
		Entitlement:  v1.NewEntitlementHandler(entitlementService, logger),
	}
}

func provideRouter(handlers api.Handlers, cfg *config.Configuration, logger *logger.Logger) *gin.Engine {
	return api.NewRouter(handlers, cfg, logger)
}

func provideTemporalConfig(cfg *config.Configuration) *config.TemporalConfig {
	return &cfg.Temporal
}

func provideTemporalService(cfg *config.TemporalConfig, log *logger.Logger) (*service.TemporalService, error) {
	return service.NewTemporalService(cfg, log)
}

func startServer(
	lc fx.Lifecycle,
	cfg *config.Configuration,
	r *gin.Engine,
	consumer kafka.MessageConsumer,
	eventRepo events.Repository,
	temporalClient *temporal.TemporalClient,
	temporalService *service.TemporalService,
	webhookService *webhook.WebhookService,
	router *pubsubRouter.Router,
	log *logger.Logger,
) {
	mode := cfg.Deployment.Mode
	if mode == "" {
		mode = types.ModeLocal
	}
	switch mode {
	case types.ModeLocal:
		startAPIServer(lc, r, cfg, log)
		startConsumer(lc, consumer, eventRepo, cfg, log)
		startMessageRouter(lc, router, webhookService, log)
		startTemporalWorker(lc, temporalClient, &cfg.Temporal, log)
	case types.ModeAPI:
		startAPIServer(lc, r, cfg, log)
		startMessageRouter(lc, router, webhookService, log)

	case types.ModeTemporalWorker:
		startTemporalWorker(lc, temporalClient, &cfg.Temporal, log)
	case types.ModeConsumer:
		if consumer == nil {
			log.Fatal("Kafka consumer required for consumer mode")
		}
		startConsumer(lc, consumer, eventRepo, cfg, log)
	case types.ModeAWSLambdaAPI:
		startAWSLambdaAPI(r)
		startMessageRouter(lc, router, webhookService, log)
	case types.ModeAWSLambdaConsumer:
		startAWSLambdaConsumer(eventRepo, log)
	default:
		log.Fatalf("Unknown deployment mode: %s", mode)
	}
}

func startTemporalWorker(
	lc fx.Lifecycle,
	temporalClient *temporal.TemporalClient,
	cfg *config.TemporalConfig,
	log *logger.Logger,
) {
	worker := temporal.NewWorker(temporalClient, *cfg, log)
	worker.RegisterWithLifecycle(lc)
}

func startAPIServer(
	lc fx.Lifecycle,
	r *gin.Engine,
	cfg *config.Configuration,
	log *logger.Logger,
) {
	log.Info("Registering API server start hook")
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info("Starting API server...")
			go func() {
				if err := r.Run(cfg.Server.Address); err != nil {
					log.Fatalf("Failed to start server: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down server...")
			return nil
		},
	})
}

func startConsumer(
	lc fx.Lifecycle,
	consumer kafka.MessageConsumer,
	eventRepo events.Repository,
	cfg *config.Configuration,
	log *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go consumeMessages(consumer, eventRepo, cfg.Kafka.Topic, log)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down consumer...")
			return nil
		},
	})
}

func startAWSLambdaAPI(r *gin.Engine) {
	ginLambda := ginadapter.New(r)
	lambda.Start(ginLambda.ProxyWithContext)
}

func startAWSLambdaConsumer(eventRepo events.Repository, log *logger.Logger) {
	handler := func(ctx context.Context, kafkaEvent lambdaEvents.KafkaEvent) error {
		log.Debugf("Received Kafka event: %+v", kafkaEvent)

		for _, record := range kafkaEvent.Records {
			for _, r := range record {
				log.Debugf("Processing record: topic=%s, partition=%d, offset=%d",
					r.Topic, r.Partition, r.Offset)

				// TODO decide the repository to use based on the event topic and properties
				// For now we will use the event repository from the events topic

				// Decode base64 payload first
				decodedPayload, err := base64.StdEncoding.DecodeString(string(r.Value))
				if err != nil {
					log.Errorf("Failed to decode base64 payload: %v", err)
					continue
				}

				var event events.Event
				if err := json.Unmarshal(decodedPayload, &event); err != nil {
					log.Errorf("Failed to unmarshal event: %v, payload: %s", err, decodedPayload)
					continue // Skip invalid messages
				}

				if err := eventRepo.InsertEvent(ctx, &event); err != nil {
					log.Errorf("Failed to insert event: %v, event: %+v", err, event)
					// TODO: Handle error and decide if we should retry or send to DLQ
					continue
				}

				log.Infof("Successfully processed event: topic=%s, partition=%d, offset=%d",
					r.Topic, r.Partition, r.Offset)
			}
		}
		return nil
	}

	lambda.Start(handler)
}

func consumeMessages(consumer kafka.MessageConsumer, eventRepo events.Repository, topic string, log *logger.Logger) {
	messages, err := consumer.Subscribe(topic)
	if err != nil {
		log.Fatalf("Failed to subscribe to topic %s: %v", topic, err)
	}

	for msg := range messages {
		var event events.Event
		if err := json.Unmarshal(msg.Payload, &event); err != nil {
			log.Errorf("Failed to unmarshal event: %v, payload: %s", err, string(msg.Payload))
			msg.Ack() // Acknowledge invalid messages
			continue
		}

		log.Debugf("Starting to process event: %+v", event)

		if err := eventRepo.InsertEvent(context.Background(), &event); err != nil {
			log.Errorf("Failed to insert event: %v, event: %+v", err, event)
			// TODO: Handle error and decide if we should retry or send to DLQ
		}
		msg.Ack()
		log.Debugf(
			"Successfully processed event with lag : %v ms : %+v",
			time.Since(event.Timestamp).Milliseconds(), event,
		)
	}
}

func startMessageRouter(
	lc fx.Lifecycle,
	router *pubsubRouter.Router,
	webhookService *webhook.WebhookService,
	logger *logger.Logger,
) {
	// Register handlers before starting the router
	// webhookService.RegisterHandler(router)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info("starting message router")
			go func() {
				if err := router.Run(); err != nil {
					logger.Errorw("message router failed", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("stopping message router")
			return router.Close()
		},
	})
}
