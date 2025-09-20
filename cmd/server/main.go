package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
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
	"github.com/flexprice/flexprice/internal/pyroscope"
	"github.com/flexprice/flexprice/internal/repository"
	s3 "github.com/flexprice/flexprice/internal/s3"
	"github.com/flexprice/flexprice/internal/sentry"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/svix"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/temporal/worker"
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
	"github.com/flexprice/flexprice/internal/domain/proration"
	"github.com/flexprice/flexprice/internal/security"
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
			// Validator
			validator.NewValidator,

			// Config
			config.NewConfig,

			// Logger
			logger.NewLogger,

			// Security
			security.NewEncryptionService,

			// storage
			s3.NewService,

			// Monitoring
			sentry.NewSentryService,
			pyroscope.NewPyroscopeService,

			// Cache
			cache.Initialize,
			cache.NewInMemoryCache,

			// Postgres
			postgres.NewEntClient,
			postgres.NewClient,

			// Clickhouse
			clickhouse.NewClickHouseStore,

			// Typst
			typst.DefaultCompiler,

			// Pdf generation
			pdf.NewGenerator,

			// Optional DBs
			dynamodb.NewClient,

			// Producers and Consumers
			kafka.NewProducer,
			kafka.NewConsumer,

			// Event Publisher
			publisher.NewEventPublisher,

			// HTTP Client
			httpclient.NewDefaultClient,

			// Svix
			svix.NewClient,

			// Repositories
			repository.NewEventRepository,
			repository.NewProcessedEventRepository,
			repository.NewFeatureUsageRepository,
			repository.NewMeterRepository,
			repository.NewUserRepository,
			repository.NewAuthRepository,
			repository.NewPriceRepository,
			repository.NewCustomerRepository,
			repository.NewPlanRepository,
			repository.NewSubscriptionRepository,
			repository.NewSubscriptionScheduleRepository,
			repository.NewWalletRepository,
			repository.NewTenantRepository,
			repository.NewEnvironmentRepository,
			repository.NewInvoiceRepository,
			repository.NewFeatureRepository,
			repository.NewEntitlementRepository,
			repository.NewPaymentRepository,
			repository.NewTaskRepository,
			repository.NewTaxAppliedRepository,
			repository.NewSecretRepository,
			repository.NewCreditGrantRepository,
			repository.NewCostSheetRepository,
			repository.NewCreditGrantApplicationRepository,
			repository.NewCreditNoteRepository,
			repository.NewCreditNoteLineItemRepository,
			repository.NewConnectionRepository,
			repository.NewEntityIntegrationMappingRepository,
			repository.NewTaxRateRepository,
			repository.NewTaxAssociationRepository,
			repository.NewCouponRepository,
			repository.NewCouponAssociationRepository,
			repository.NewCouponApplicationRepository,
			repository.NewPriceUnitRepository,
			repository.NewAddonRepository,
			repository.NewAddonAssociationRepository,
			repository.NewSubscriptionLineItemRepository,
			repository.NewSettingsRepository,
			repository.NewAlertRepository,

			// PubSub
			pubsubRouter.NewRouter,

			// Proration
			proration.NewCalculator,
		),
	)

	// Webhook module (must be initialised before services)
	opts = append(opts, webhook.Module)

	// Service layer
	opts = append(opts,
		fx.Provide(
			// Services
			service.NewServiceParams,
			service.NewTenantService,
			service.NewAuthService,
			service.NewUserService,
			service.NewEnvAccessService,
			service.NewEnvironmentService,
			service.NewMeterService,
			service.NewEventService,
			service.NewEventPostProcessingService,
			service.NewFeatureUsageTrackingService,
			service.NewPriceService,
			service.NewCustomerService,
			service.NewPlanService,
			service.NewSubscriptionService,
			service.NewWalletService,
			service.NewInvoiceService,
			service.NewFeatureService,
			service.NewEntitlementService,
			service.NewPaymentService,
			service.NewPaymentProcessorService,
			service.NewTaskService,
			service.NewSecretService,
			service.NewOnboardingService,
			service.NewBillingService,
			service.NewCreditGrantService,
			service.NewCostSheetService,
			service.NewCreditNoteService,
			service.NewConnectionService,
			service.NewStripeService,
			service.NewEntityIntegrationMappingService,
			service.NewIntegrationService,
			service.NewTaxService,
			service.NewCouponService,
			service.NewPriceUnitService,
			service.NewAddonService,
			service.NewSettingsService,
			service.NewSubscriptionChangeService,
			service.NewAlertService,
		),
	)

	// API layer
	opts = append(opts,
		fx.Provide(
			// Temporal components
			provideTemporalConfig,
			provideTemporalClient,
			provideTemporalWorkerManager,
			provideTemporalService,

			// API components
			provideHandlers,
			provideRouter,
		),
		fx.Invoke(
			sentry.RegisterHooks,
			pyroscope.RegisterHooks,
			startServer,
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
	eventPostProcessingService service.EventPostProcessingService,
	environmentService service.EnvironmentService,
	authService service.AuthService,
	userService service.UserService,
	priceService service.PriceService,
	customerService service.CustomerService,
	planService service.PlanService,
	subscriptionService service.SubscriptionService,
	walletService service.WalletService,
	tenantService service.TenantService,
	invoiceService service.InvoiceService,
	temporalService temporalservice.TemporalService,
	featureService service.FeatureService,
	entitlementService service.EntitlementService,
	paymentService service.PaymentService,
	paymentProcessorService service.PaymentProcessorService,
	taskService service.TaskService,
	secretService service.SecretService,
	onboardingService service.OnboardingService,
	billingService service.BillingService,
	creditGrantService service.CreditGrantService,
	costSheetService service.CostSheetService,
	creditNoteService service.CreditNoteService,
	stripeService *service.StripeService,
	connectionService service.ConnectionService,
	entityIntegrationMappingService service.EntityIntegrationMappingService,
	integrationService service.IntegrationService,
	priceUnitService *service.PriceUnitService,
	svixClient *svix.Client,
	taxService service.TaxService,
	couponService service.CouponService,
	addonService service.AddonService,
	settingsService service.SettingsService,
	subscriptionChangeService service.SubscriptionChangeService,
	featureUsageTrackingService service.FeatureUsageTrackingService,
	alertService service.AlertService,
) api.Handlers {
	return api.Handlers{
		Events:                   v1.NewEventsHandler(eventService, eventPostProcessingService, featureUsageTrackingService, logger),
		Meter:                    v1.NewMeterHandler(meterService, logger),
		Auth:                     v1.NewAuthHandler(cfg, authService, logger),
		User:                     v1.NewUserHandler(userService, logger),
		Environment:              v1.NewEnvironmentHandler(environmentService, logger),
		Health:                   v1.NewHealthHandler(logger),
		Price:                    v1.NewPriceHandler(priceService, logger),
		Customer:                 v1.NewCustomerHandler(customerService, billingService, logger),
		Plan:                     v1.NewPlanHandler(planService, entitlementService, creditGrantService, temporalService, logger),
		Subscription:             v1.NewSubscriptionHandler(subscriptionService, logger),
		SubscriptionPause:        v1.NewSubscriptionPauseHandler(subscriptionService, logger),
		SubscriptionChange:       v1.NewSubscriptionChangeHandler(subscriptionChangeService, logger),
		Wallet:                   v1.NewWalletHandler(walletService, logger),
		Tenant:                   v1.NewTenantHandler(tenantService, logger),
		Invoice:                  v1.NewInvoiceHandler(invoiceService, logger),
		Feature:                  v1.NewFeatureHandler(featureService, logger),
		Entitlement:              v1.NewEntitlementHandler(entitlementService, logger),
		Payment:                  v1.NewPaymentHandler(paymentService, paymentProcessorService, logger),
		Task:                     v1.NewTaskHandler(taskService, temporalService, logger),
		Secret:                   v1.NewSecretHandler(secretService, logger),
		Tax:                      v1.NewTaxHandler(taxService, logger),
		Onboarding:               v1.NewOnboardingHandler(onboardingService, logger),
		CronSubscription:         cron.NewSubscriptionHandler(subscriptionService, logger),
		CronWallet:               cron.NewWalletCronHandler(logger, walletService, tenantService, environmentService),
		CreditGrant:              v1.NewCreditGrantHandler(creditGrantService, logger),
		CostSheet:                v1.NewCostSheetHandler(costSheetService, logger),
		CronCreditGrant:          cron.NewCreditGrantCronHandler(creditGrantService, logger),
		CreditNote:               v1.NewCreditNoteHandler(creditNoteService, logger),
		Connection:               v1.NewConnectionHandler(connectionService, logger),
		EntityIntegrationMapping: v1.NewEntityIntegrationMappingHandler(entityIntegrationMappingService, logger),
		Integration:              v1.NewIntegrationHandler(integrationService, logger),
		PriceUnit:                v1.NewPriceUnitHandler(priceUnitService, logger),
		Webhook:                  v1.NewWebhookHandler(cfg, svixClient, logger, stripeService),
		Coupon:                   v1.NewCouponHandler(couponService, logger),
		Addon:                    v1.NewAddonHandler(addonService, logger),
		Settings:                 v1.NewSettingsHandler(settingsService, logger),
		CronAlert:                cron.NewAlertCronHandler(alertService, logger),
	}
}

func provideRouter(handlers api.Handlers, cfg *config.Configuration, logger *logger.Logger, secretService service.SecretService, envAccessService service.EnvAccessService) *gin.Engine {
	return api.NewRouter(handlers, cfg, logger, secretService, envAccessService)
}

func provideTemporalConfig(cfg *config.Configuration) *config.TemporalConfig {
	return &cfg.Temporal
}

func provideTemporalClient(cfg *config.TemporalConfig, log *logger.Logger) (client.TemporalClient, error) {
	log.Info("Initializing Temporal client", "address", cfg.Address, "namespace", cfg.Namespace)

	// Use default options and merge with config
	options := models.DefaultClientOptions()
	if cfg.Address != "" {
		options.Address = cfg.Address
	}
	if cfg.Namespace != "" {
		options.Namespace = cfg.Namespace
	}
	if cfg.APIKey != "" {
		options.APIKey = cfg.APIKey
	}
	options.TLS = cfg.TLS

	// Create temporal client directly
	temporalClient, err := client.NewTemporalClient(options, log)
	if err != nil {
		log.Error("Failed to create Temporal client", "error", err)
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	log.Info("Temporal client created successfully")
	return temporalClient, nil
}

func provideTemporalWorkerManager(temporalClient client.TemporalClient, log *logger.Logger) worker.TemporalWorkerManager {
	return worker.NewTemporalWorkerManager(temporalClient, log)
}

func provideTemporalService(temporalClient client.TemporalClient, workerManager worker.TemporalWorkerManager, log *logger.Logger) temporalservice.TemporalService {
	service := temporalservice.NewTemporalService(temporalClient, workerManager, log)
	service.Start(context.Background())
	return service
}

func startServer(
	lc fx.Lifecycle,
	cfg *config.Configuration,
	r *gin.Engine,
	consumer kafka.MessageConsumer,
	eventRepo events.Repository,
	temporalClient client.TemporalClient,
	temporalService temporalservice.TemporalService,
	webhookService *webhook.WebhookService,
	router *pubsubRouter.Router,
	onboardingService service.OnboardingService,
	log *logger.Logger,
	sentryService *sentry.Service,
	eventPostProcessingSvc service.EventPostProcessingService,
	featureUsageSvc service.FeatureUsageTrackingService,
	params service.ServiceParams,
) {
	mode := cfg.Deployment.Mode
	if mode == "" {
		mode = types.ModeLocal
	}

	switch mode {
	case types.ModeLocal:
		if consumer == nil {
			log.Fatal("Kafka consumer required for local mode")
		}
		startAPIServer(lc, r, cfg, log)
		startConsumer(lc, consumer, eventRepo, cfg, log, sentryService, eventPostProcessingSvc)

		// Register all handlers and start router once
		registerRouterHandlers(router, webhookService, onboardingService, eventPostProcessingSvc, featureUsageSvc, cfg, true)
		startRouter(lc, router, log)
		startTemporalWorker(lc, temporalService, params)
	case types.ModeAPI:
		startAPIServer(lc, r, cfg, log)

		// Register all handlers and start router once
		registerRouterHandlers(router, webhookService, onboardingService, eventPostProcessingSvc, featureUsageSvc, cfg, false)
		startRouter(lc, router, log)

	case types.ModeTemporalWorker:
		startTemporalWorker(lc, temporalService, params)
	case types.ModeConsumer:
		if consumer == nil {
			log.Fatal("Kafka consumer required for consumer mode")
		}
		startConsumer(lc, consumer, eventRepo, cfg, log, sentryService, eventPostProcessingSvc)

		// Register all handlers and start router once
		registerRouterHandlers(router, webhookService, onboardingService, eventPostProcessingSvc, featureUsageSvc, cfg, true)
		startRouter(lc, router, log)
	case types.ModeAWSLambdaAPI:
		startAWSLambdaAPI(r)

		// Register basic handlers and start router once
		registerRouterHandlers(router, webhookService, onboardingService, eventPostProcessingSvc, featureUsageSvc, cfg, false)
		startRouter(lc, router, log)
	case types.ModeAWSLambdaConsumer:
		startAWSLambdaConsumer(eventRepo, cfg, log, sentryService, eventPostProcessingSvc)
	default:
		log.Fatalf("Unknown deployment mode: %s", mode)
	}
}

func startTemporalWorker(
	lc fx.Lifecycle,
	temporalService temporalservice.TemporalService,
	params service.ServiceParams,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			// Register workflows and activities first (this creates the workers)
			if err := temporal.RegisterWorkflowsAndActivities(temporalService, params); err != nil {
				return fmt.Errorf("failed to register workflows and activities: %w", err)
			}

			// Start workers for all task queues
			for _, taskQueue := range types.GetAllTaskQueues() {
				if err := temporalService.StartWorker(taskQueue); err != nil {
					return fmt.Errorf("failed to start worker for task queue %s: %w", taskQueue.String(), err)
				}
			}

			return nil
		},
		OnStop: func(ctx context.Context) error {
			return temporalService.StopAllWorkers()
		},
	})
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
	sentryService *sentry.Service,
	eventPostProcessingSvc service.EventPostProcessingService,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			go consumeMessages(consumer, eventRepo, cfg, log, sentryService, eventPostProcessingSvc)
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

func startAWSLambdaConsumer(eventRepo events.Repository, cfg *config.Configuration, log *logger.Logger, sentryService *sentry.Service, eventPostProcessingSvc service.EventPostProcessingService) {
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

				if err := handleEventConsumption(ctx, cfg, log, eventRepo, decodedPayload, sentryService, eventPostProcessingSvc); err != nil {
					log.Errorf("Failed to process event: %v, payload: %s", err, string(decodedPayload))
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

func consumeMessages(consumer kafka.MessageConsumer, eventRepo events.Repository, cfg *config.Configuration, log *logger.Logger, sentryService *sentry.Service, eventPostProcessingSvc service.EventPostProcessingService) {
	messages, err := consumer.Subscribe(cfg.Kafka.Topic)
	if err != nil {
		log.Fatalf("Failed to subscribe to topic %s: %v", cfg.Kafka.Topic, err)
	}

	log.Infof("Successfully subscribed to topic %s", cfg.Kafka.Topic)

	for msg := range messages {
		ctx := context.Background()
		if err := handleEventConsumption(ctx, cfg, log, eventRepo, msg.Payload, sentryService, eventPostProcessingSvc); err != nil {
			log.Errorf("Failed to process event: %v, payload: %s", err, string(msg.Payload))

			// Don't immediately Nack, retry processing a few times before giving up
			if shouldRetry(err) {
				// Only Nack (negative acknowledge) if it's a retriable error
				// This will cause the message to be redelivered
				log.Warnf("Nacking message to retry later")
				msg.Nack()
			} else {
				// For non-retriable errors, we acknowledge the message to avoid
				// endless reprocessing of problematic messages
				log.Warnf("Error not retriable, acknowledging message to avoid blocking: %v", err)
				msg.Ack()

				// Record this message as a dead letter for later inspection
				recordDeadLetterMessage(ctx, msg.Payload, err, log, sentryService)
			}
			continue
		}

		// Successfully processed the message
		log.Debugf("Successfully processed message, acknowledging it")
		msg.Ack()
	}
}

// shouldRetry determines if an error should trigger a message retry
func shouldRetry(err error) bool {
	// Add logic to determine if this error is transient and worth retrying
	// Examples: database connection issues, temporary unavailability, etc.

	// For now, retry all errors except parsing errors which are not likely to succeed on retry
	if strings.Contains(err.Error(), "unmarshal") ||
		strings.Contains(err.Error(), "parse") {
		return false
	}
	return true
}

// recordDeadLetterMessage records problematic messages for later inspection
func recordDeadLetterMessage(_ context.Context, payload []byte, processingErr error, log *logger.Logger, sentryService *sentry.Service) {
	// In a production system, you would send this to a dead letter queue
	// or record it in a database table for later inspection

	// For now, just log it with a special tag for monitoring
	log.Errorf("DEAD_LETTER_MESSAGE: %v, payload: %s", processingErr, string(payload))

	// Capture the error in Sentry for monitoring
	if sentryService != nil {
		sentryService.CaptureException(fmt.Errorf("dead letter message: %w", processingErr))
	}
}

func handleEventConsumption(ctx context.Context, cfg *config.Configuration, log *logger.Logger, eventRepo events.Repository, payload []byte, sentryService *sentry.Service, eventPostProcessingSvc service.EventPostProcessingService) error {
	// Start a transaction for this event processing
	transaction, ctx := sentryService.StartTransaction(ctx, "event.process")
	if transaction != nil {
		defer transaction.Finish()
	}

	// Start a Kafka consumer span
	kafkaSpan, ctx := sentryService.StartKafkaConsumerSpan(ctx, cfg.Kafka.Topic)
	if kafkaSpan != nil {
		defer kafkaSpan.Finish()
	}

	var event events.Event
	if err := json.Unmarshal(payload, &event); err != nil {
		log.Errorf("Failed to unmarshal event: %v, payload: %s", err, string(payload))
		sentryService.CaptureException(err)
		return err
	}

	log.Debugf("Starting to process event: %+v", event)

	// Start an event processing span
	// eventSpan, ctx := sentryService.MonitorEventProcessing(
	// 	ctx,
	// 	event.EventName,
	// 	event.Timestamp,
	// 	map[string]interface{}{
	// 		"event_id":       event.ID,
	// 		"tenant_id":      event.TenantID,
	// 		"source":         event.Source,
	// 		"environment_id": event.EnvironmentID,
	// 	},
	// )
	// if eventSpan != nil {
	// 	defer eventSpan.Finish()
	// }

	eventsToInsert := []*events.Event{&event}

	if cfg.Billing.TenantID != "" {
		// Create a billing copy with the tenant ID as the external customer ID
		billingEvent := events.NewEvent(
			"tenant_event", // Standardized event name for billing
			cfg.Billing.TenantID,
			event.TenantID, // Use original tenant ID as external customer ID
			map[string]interface{}{
				"original_event_id":   event.ID,
				"original_event_name": event.EventName,
				"original_timestamp":  event.Timestamp,
				"tenant_id":           event.TenantID,
				"source":              event.Source,
			},
			time.Now(),
			"", // Customer ID will be looked up by external ID
			"", // Generate new ID
			"system",
			cfg.Billing.EnvironmentID,
		)
		eventsToInsert = append(eventsToInsert, billingEvent)
	}

	// Insert both events in a single operation
	if err := eventRepo.BulkInsertEvents(ctx, eventsToInsert); err != nil {
		log.Errorf("Failed to insert events: %v, original event: %+v", err, event)
		return err
	}

	// Publish event to post-processing service
	if err := eventPostProcessingSvc.PublishEvent(ctx, &event, false); err != nil {
		log.Errorf("Failed to publish event to post-processing service: %v, original event: %+v", err, event)
		return err
	}

	log.Debugf(
		"Successfully processed event with lag : %v ms : %+v",
		time.Since(event.Timestamp).Milliseconds(), event,
	)
	return nil
}

func registerRouterHandlers(
	router *pubsubRouter.Router,
	webhookService *webhook.WebhookService,
	onboardingService service.OnboardingService,
	eventPostProcessingSvc service.EventPostProcessingService,
	featureUsageSvc service.FeatureUsageTrackingService,
	cfg *config.Configuration,
	includeProcessingHandlers bool,
) {
	// Always register these basic handlers
	webhookService.RegisterHandler(router)
	onboardingService.RegisterHandler(router)

	// Only register processing handlers when needed
	if includeProcessingHandlers {
		eventPostProcessingSvc.RegisterHandler(router, cfg)
		featureUsageSvc.RegisterHandler(router, cfg)
	}
}

func startRouter(
	lc fx.Lifecycle,
	router *pubsubRouter.Router,
	logger *logger.Logger,
) {
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
