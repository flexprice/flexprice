package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api"
	"github.com/flexprice/flexprice/internal/api/cron"
	v1 "github.com/flexprice/flexprice/internal/api/v1"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/dynamodb"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/kafka"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/pdf"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/publisher"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/repository"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/typst"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/flexprice/flexprice/internal/webhook"
	"go.uber.org/fx"

	lambdaEvents "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	ginadapter "github.com/awslabs/aws-lambda-go-api-proxy/gin"
	_ "github.com/flexprice/flexprice/docs/swagger"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/gin-gonic/gin"
)

func init() {
	// Set UTC timezone for the entire application
	time.Local = time.UTC
}

func main() {
	// Initialize Fx application
	var opts []fx.Option

	// Core dependencies for various services
	opts = append(opts,
		fx.Provide(
			validator.NewValidator,
			config.NewConfig,
			logger.NewLogger,
			sentry.NewSentryService,
			cache.Initialize,
			postgres.NewEntClient,
			clickhouse.NewClickHouseStore,
			typst.DefaultCompiler,
			pdf.NewGenerator,
			dynamodb.NewClient,
			kafka.NewProducer,
			kafka.NewConsumer,
			publisher.NewEventPublisher,
			httpclient.NewDefaultClient,
			repository.NewEventRepository,
			repository.NewMeterRepository,
			repository.NewUserRepository,
			repository.NewPriceRepository,
			repository.NewSubscriptionRepository,
			repository.NewWalletRepository,
			repository.NewInvoiceRepository,
			repository.NewFeatureRepository,
			repository.NewEntitlementRepository,
			pubsubRouter.NewRouter,
			provideTemporalClient,
			provideTemporalService,
		),
	)

	// Webhook module (must be initialized before services)
	opts = append(opts, webhook.Module)

	// Service layer
	opts = append(opts,
		fx.Provide(
			service.NewServiceParams,
			service.NewTenantService,
			service.NewAuthService,
			service.NewUserService,
			service.NewEnvironmentService,
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
			service.NewPaymentService,
			service.NewTaskService,
			service.NewSecretService,
			service.NewOnboardingService,
			service.NewBillingService,
		),
	)

	// API and Temporal setup
	opts = append(opts,
		fx.Provide(
			provideHandlers,
			provideRouter,
			provideTemporalConfig,
		),
		fx.Invoke(
			sentry.RegisterHooks,
			startServer,
		),
	)

	app := fx.New(opts...)
	app.Run()
}

// Provide Handlers to API routes
func provideHandlers(
	cfg *config.Configuration,
	logger *logger.Logger,
	meterService service.MeterService,
	eventService service.EventService,
	// other services...
) api.Handlers {
	return api.Handlers{
		Events:            v1.NewEventsHandler(eventService, logger),
		Meter:             v1.NewMeterHandler(meterService, logger),
		// other handlers...
	}
}

// API Router Setup
func provideRouter(handlers api.Handlers, cfg *config.Configuration, logger *logger.Logger) *gin.Engine {
	return api.NewRouter(handlers, cfg, logger)
}

func provideTemporalConfig(cfg *config.Configuration) *config.TemporalConfig {
	return &cfg.Temporal
}

func provideTemporalClient(cfg *config.TemporalConfig, log *logger.Logger) (*temporal.TemporalClient, error) {
	return temporal.NewTemporalClient(cfg, log)
}

func provideTemporalService(temporalClient *temporal.TemporalClient, cfg *config.TemporalConfig, log *logger.Logger) (*temporal.Service, error) {
	return temporal.NewService(temporalClient, cfg, log)
}

// Service to handle server startup
func startServer(
	lc fx.Lifecycle,
	cfg *config.Configuration,
	r *gin.Engine,
	consumer kafka.MessageConsumer,
	eventRepo events.Repository,
	temporalClient *temporal.TemporalClient,
	webhookService *webhook.WebhookService,
	router *pubsubRouter.Router,
	log *logger.Logger,
) {
	mode := cfg.Deployment.Mode
	if mode == "" {
		mode = types.ModeLocal
	}

	// Handle different modes
	switch mode {
	case types.ModeLocal:
		if consumer == nil {
			log.Fatal("Kafka consumer required for local mode")
		}
		startAPIServer(lc, r, cfg, log)
		startConsumer(lc, consumer, eventRepo, cfg, log)
		startMessageRouter(lc, router, webhookService, log)

	case types.ModeAPI:
		startAPIServer(lc, r, cfg, log)
		startMessageRouter(lc, router, webhookService, log)

	case types.ModeTemporalWorker:
		startTemporalWorker(lc, temporalClient, &cfg.Temporal, log)
	default:
		log.Fatalf("Unknown deployment mode: %s", mode)
	}
}

// API Server Handler
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

// Kafka Consumer for handling messages
func startConsumer(
	lc fx.Lifecycle,
	consumer kafka.MessageConsumer,
	eventRepo events.Repository,
	cfg *config.Configuration,
	log *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go consumeMessages(consumer, eventRepo, cfg, log)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Shutting down consumer...")
			return nil
		},
	})
}

// Handle consumption of Kafka messages
func consumeMessages(consumer kafka.MessageConsumer, eventRepo events.Repository, cfg *config.Configuration, log *logger.Logger) {
	messages, err := consumer.Subscribe(cfg.Kafka.Topic)
	if err != nil {
		log.Fatalf("Failed to subscribe to topic %s: %v", cfg.Kafka.Topic, err)
	}

	for msg := range messages {
		if err := handleEventConsumption(cfg, log, eventRepo, msg.Payload); err != nil {
			log.Errorf("Failed to process event: %v, payload: %s", err, string(msg.Payload))
			msg.Nack()
			continue
		}
		msg.Ack()
	}
}

// Handle event consumption and insertion into the event repo
func handleEventConsumption(cfg *config.Configuration, log *logger.Logger, eventRepo events.Repository, payload []byte) error {
	var event events.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		log.Errorf("Failed to unmarshal event: %v, payload: %s", err, string(payload))
		return err
	}

	log.Debugf("Starting to process event: %+v", event)

	eventsToInsert := []*events.Event{&event}

	// Insert events in a single operation
	if err := eventRepo.BulkInsertEvents(context.Background(), eventsToInsert); err != nil {
		log.Errorf("Failed to insert events: %v, original event: %+v", err, event)
		return err
	}

	log.Debugf("Successfully processed event with lag: %v ms : %+v", time.Since(event.Timestamp).Milliseconds(), event)
	return nil
}

// Start Message Router
func startMessageRouter(
	lc fx.Lifecycle,
	router *pubsubRouter.Router,
	webhookService *webhook.WebhookService,
	log *logger.Logger,
) {
	// Register handlers before starting the router
	webhookService.RegisterHandler(router)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info("Starting message router")
			go func() {
				if err := router.Run(); err != nil {
					log.Errorw("Message router failed", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info("Stopping message router")
			return router.Close()
		},
	})
}

func startAWSLambdaAPI(r *gin.Engine) {
	ginLambda := ginadapter.New(r)
	lambda.Start(ginLambda.ProxyWithContext)
}

func startAWSLambdaConsumer(eventRepo events.Repository, cfg *config.Configuration, log *logger.Logger) {
	handler := func(ctx context.Context, kafkaEvent lambdaEvents.KafkaEvent) error {
		log.Debugf("Received Kafka event: %+v", kafkaEvent)

		for _, record := range kafkaEvent.Records {
			for _, r := range record {
				log.Debugf("Processing record: topic=%s, partition=%d, offset=%d", r.Topic, r.Partition, r.Offset)
				// Decode base64 payload first
				decodedPayload, err := base64.StdEncoding.DecodeString(string(r.Value))
				if err != nil {
					log.Errorf("Failed to decode base64 payload: %v", err)
					continue
				}

				if err := handleEventConsumption(cfg, log, eventRepo, decodedPayload); err != nil {
					log.Errorf("Failed to process event: %v, payload: %s", err, string(decodedPayload))
					continue
				}

				log.Infof("Successfully processed event: topic=%s, partition=%d, offset=%d", r.Topic, r.Partition, r.Offset)
			}
		}
		return nil
	}

	lambda.Start(handler)
}
