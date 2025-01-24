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
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/workflow"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.uber.org/fx"

	lambdaEvents "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
	_ "github.com/flexprice/flexprice/docs/swagger"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
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
	var opts []fx.Option
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

			// Temporal Client
			temporal.NewClient,

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

			// Services
			service.NewMeterService,
			service.NewEventService,
			service.NewUserService,
			service.NewAuthService,
			service.NewPriceService,
			service.NewCustomerService,
			service.NewPlanService,
			service.NewSubscriptionService,
			service.NewWalletService,
			service.NewTenantService,
			service.NewInvoiceService,

			// Handlers
			provideHandlers,

			// Router
			provideRouter,
		),
		fx.Invoke(
			sentry.RegisterHooks,
			startServer,
			registerTemporalWorker,
		),
	)

	app := fx.New(opts...)
	app.Run()
}

func registerTemporalWorker(
	lc fx.Lifecycle,
	temporalClient client.Client,
	subscriptionService service.SubscriptionService,
	logger *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Create worker
			w := worker.New(temporalClient, "billing-period-queue", worker.Options{})

			// Register workflow and activity
			w.RegisterWorkflow(workflow.UpdateBillingPeriodsWorkflow)
			w.RegisterActivity(workflow.UpdateBillingPeriodsActivity)

			// Start worker in a goroutine
			go func() {
				if err := w.Run(worker.InterruptCh()); err != nil {
					logger.Fatal("Unable to start worker", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info("Shutting down Temporal worker...")
			return nil
		},
	})
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
) api.Handlers {
	return api.Handlers{
		Events:       v1.NewEventsHandler(eventService, logger),
		Meter:        v1.NewMeterHandler(meterService, logger),
		Auth:         v1.NewAuthHandler(cfg, authService, logger),
		User:         v1.NewUserHandler(userService, logger),
		Price:        v1.NewPriceHandler(priceService, logger),
		Customer:     v1.NewCustomerHandler(customerService, logger),
		Plan:         v1.NewPlanHandler(planService, logger),
		Subscription: v1.NewSubscriptionHandler(subscriptionService, logger),
		Wallet:       v1.NewWalletHandler(walletService, logger),
		Tenant:       v1.NewTenantHandler(tenantService, logger),
		Cron:         cron.NewSubscriptionHandler(subscriptionService, logger),
		Invoice:      v1.NewInvoiceHandler(invoiceService, logger),
	}
}

func provideRouter(handlers api.Handlers, cfg *config.Configuration, logger *logger.Logger) *gin.Engine {
	return api.NewRouter(handlers, cfg, logger)
}

func startServer(
	lc fx.Lifecycle,
	cfg *config.Configuration,
	r *gin.Engine,
	consumer kafka.MessageConsumer,
	eventRepo events.Repository,
	temporalClient client.Client,
	subscriptionService service.SubscriptionService,
	log *logger.Logger,
) {
	mode := cfg.Deployment.Mode

	switch mode {
	case types.ModeLocal:
		if consumer == nil {
			log.Fatal("Kafka consumer required for local mode")
		}
		startAPIServer(lc, r, cfg, log)
		startConsumer(lc, consumer, eventRepo, cfg, log)
		startTemporalWorker(lc, temporalClient, subscriptionService, log)
	case types.ModeAPI:
		startAPIServer(lc, r, cfg, log)
	case types.ModeConsumer:
		if consumer == nil {
			log.Fatal("Kafka consumer required for consumer mode")
		}
		startConsumer(lc, consumer, eventRepo, cfg, log)
	case types.ModeTemporalWorker:
		startTemporalWorker(lc, temporalClient, subscriptionService, log)
	case types.ModeAWSLambdaAPI:
		startAWSLambdaAPI(r)
	case types.ModeAWSLambdaConsumer:
		startAWSLambdaConsumer(eventRepo, log)
	default:
		log.Fatalf("Unknown deployment mode: %s", mode)
	}
}

func startAPIServer(
	lc fx.Lifecycle,
	r *gin.Engine,
	cfg *config.Configuration,
	log *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
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

				decodedPayload, err := base64.StdEncoding.DecodeString(string(r.Value))
				if err != nil {
					log.Errorf("Failed to decode base64 payload: %v", err)
					continue
				}

				var event events.Event
				if err := json.Unmarshal(decodedPayload, &event); err != nil {
					log.Errorf("Failed to unmarshal event: %v, payload: %s", err, decodedPayload)
					continue
				}

				if err := eventRepo.InsertEvent(ctx, &event); err != nil {
					log.Errorf("Failed to insert event: %v, event: %+v", err, event)
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
			msg.Ack()
			continue
		}

		log.Debugf("Starting to process event: %+v", event)

		if err := eventRepo.InsertEvent(context.Background(), &event); err != nil {
			log.Errorf("Failed to insert event: %v, event: %+v", err, event)
		}
		msg.Ack()
		log.Debugf(
			"Successfully processed event with lag : %v ms : %+v",
			time.Since(event.Timestamp).Milliseconds(), event,
		)
	}
}

func startTemporalWorker(
	lc fx.Lifecycle,
	temporalClient client.Client,
	subscriptionService service.SubscriptionService,
	log *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			w := worker.New(temporalClient, "billing-period-queue", worker.Options{})

			// Register workflow
			w.RegisterWorkflow(workflow.UpdateBillingPeriodsWorkflow)

			// Register activity - note that we're just passing the function
			w.RegisterActivity(workflow.UpdateBillingPeriodsActivity)

			go func() {
				if err := w.Run(worker.InterruptCh()); err != nil {
					log.Fatal("Unable to start worker", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down Temporal worker...")
			return nil
		},
	})
}
