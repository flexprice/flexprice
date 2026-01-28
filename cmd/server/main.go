package main

import (
	"context"
	"fmt"
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
	kafkaPubsub "github.com/flexprice/flexprice/internal/pubsub/kafka"
	pubsubRouter "github.com/flexprice/flexprice/internal/pubsub/router"
	"github.com/flexprice/flexprice/internal/pyroscope"
	"github.com/flexprice/flexprice/internal/rbac"
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

	_ "github.com/flexprice/flexprice/docs/swagger"
	"github.com/flexprice/flexprice/internal/domain/proration"
	ee "github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/security"
	syncExport "github.com/flexprice/flexprice/internal/service/sync/export"
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

			// RBAC
			rbac.NewRBACService,

			// storage
			s3.NewService,

			// Monitoring
			sentry.NewSentryService,
			pyroscope.NewPyroscopeService,

			// Cache
			cache.Initialize,

			// Postgres
			postgres.NewEntClients,
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
			repository.NewCostSheetUsageRepository,
			repository.NewMeterRepository,
			repository.NewUserRepository,
			repository.NewAuthRepository,
			repository.NewPriceRepository,
			repository.NewCustomerRepository,
			repository.NewPlanRepository,
			repository.NewPlanPriceSyncRepository,
			repository.NewSubscriptionRepository,
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
			repository.NewCostsheetRepository,
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
			repository.NewAddonRepository,
			repository.NewAddonAssociationRepository,
			repository.NewSubscriptionLineItemRepository,
			repository.NewSubscriptionPhaseRepository,
			repository.NewSubscriptionScheduleRepository,
			repository.NewSettingsRepository,
			repository.NewAlertLogsRepository,
			repository.NewGroupRepository,
			repository.NewScheduledTaskRepository,
			repository.NewPriceUnitRepository,

			// PubSub
			pubsubRouter.NewRouter,

			// Proration
			proration.NewCalculator,
		),
	)

	// Webhook module (must be initialised before services)
	opts = append(opts, webhook.Module)

	// Provide Wallet Balance Alert PubSub
	opts = append(opts,
		fx.Provide(
			provideWalletBalanceAlertPubSub,
		),
	)

	// Service layer
	opts = append(opts,
		fx.Provide(
			// Services
			// Integration factory must be provided before service params
			integration.NewFactory,
			syncExport.NewExportService,
			service.NewServiceParams,
			service.NewOAuthService,
			service.NewTenantService,
			service.NewAuthService,
			service.NewUserService,
			service.NewEnvAccessService,
			service.NewEnvironmentService,
			service.NewMeterService,
			service.NewEventService,
			service.NewEventPostProcessingService,
			service.NewEventConsumptionService,
			service.NewFeatureUsageTrackingService,
			service.NewCostSheetUsageTrackingService,
			service.NewPriceService,
			service.NewPriceUnitService,
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
			service.NewCostsheetService,
			service.NewRevenueAnalyticsService,
			service.NewCreditNoteService,
			service.NewConnectionService,
			service.NewEntityIntegrationMappingService,
			service.NewTaxService,
			service.NewCouponService,
			service.NewAddonService,
			service.NewSettingsService,
			service.NewSubscriptionChangeService,
			service.NewSubscriptionScheduleService,
			service.NewAlertLogsService,
			service.NewGroupService,
			service.NewScheduledTaskService,
			service.NewWalletPaymentService,
			service.NewWalletBalanceAlertService,
			service.NewCustomerPortalService,
			service.NewDashboardService,

			// Enterprise (ee) services
			ee.NewEnterpriseParams,
			ee.NewCreditNoteService,
			ee.NewCreditGrantService,
			ee.NewWalletService,
			ee.NewPrepaidCreditsService,
			ee.NewBillingTimezoneService,
			ee.NewInvoiceGracePeriodService,
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
	priceUnitService service.PriceUnitService,
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
	costsheetService service.CostsheetService,
	revenueAnalyticsService service.RevenueAnalyticsService,
	creditNoteService service.CreditNoteService,
	connectionService service.ConnectionService,
	entityIntegrationMappingService service.EntityIntegrationMappingService,
	svixClient *svix.Client,
	taxService service.TaxService,
	couponService service.CouponService,
	addonService service.AddonService,
	settingsService service.SettingsService,
	subscriptionChangeService service.SubscriptionChangeService,
	subscriptionScheduleService service.SubscriptionScheduleService,
	featureUsageTrackingService service.FeatureUsageTrackingService,
	alertLogsService service.AlertLogsService,
	groupService service.GroupService,
	integrationFactory *integration.Factory,
	db postgres.IClient,
	scheduledTaskService service.ScheduledTaskService,
	rbacService *rbac.RBACService,
	oauthService service.OAuthService,
	costsheetUsageTrackingService service.CostSheetUsageTrackingService,
	customerPortalService service.CustomerPortalService,
	dashboardService service.DashboardService,
) api.Handlers {
	return api.Handlers{
		Events:                   v1.NewEventsHandler(eventService, eventPostProcessingService, featureUsageTrackingService, cfg, logger),
		Meter:                    v1.NewMeterHandler(meterService, logger),
		Auth:                     v1.NewAuthHandler(cfg, authService, logger),
		User:                     v1.NewUserHandler(userService, logger),
		Environment:              v1.NewEnvironmentHandler(environmentService, logger),
		Health:                   v1.NewHealthHandler(logger),
		Price:                    v1.NewPriceHandler(priceService, logger),
		PriceUnit:                v1.NewPriceUnitHandler(priceUnitService, logger),
		Customer:                 v1.NewCustomerHandler(customerService, billingService, logger),
		Plan:                     v1.NewPlanHandler(planService, entitlementService, creditGrantService, temporalService, logger),
		Subscription:             v1.NewSubscriptionHandler(subscriptionService, logger),
		SubscriptionPause:        v1.NewSubscriptionPauseHandler(subscriptionService, logger),
		SubscriptionChange:       v1.NewSubscriptionChangeHandler(subscriptionChangeService, logger),
		SubscriptionSchedule:     v1.NewSubscriptionScheduleHandler(subscriptionScheduleService),
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
		CronWallet:               cron.NewWalletCronHandler(logger, walletService, tenantService, environmentService, featureService, alertLogsService),
		CronInvoice:              cron.NewInvoiceHandler(invoiceService, subscriptionService, connectionService, tenantService, environmentService, integrationFactory, logger),
		CreditGrant:              v1.NewCreditGrantHandler(creditGrantService, logger),
		Costsheet:                v1.NewCostsheetHandler(costsheetService, logger),
		RevenueAnalytics:         v1.NewRevenueAnalyticsHandler(revenueAnalyticsService, costsheetUsageTrackingService, cfg, logger),
		CronCreditGrant:          cron.NewCreditGrantCronHandler(creditGrantService, logger),
		CreditNote:               v1.NewCreditNoteHandler(creditNoteService, logger),
		Connection:               v1.NewConnectionHandler(connectionService, logger),
		EntityIntegrationMapping: v1.NewEntityIntegrationMappingHandler(entityIntegrationMappingService, logger),
		Webhook:                  v1.NewWebhookHandler(cfg, svixClient, logger, integrationFactory, customerService, paymentService, invoiceService, planService, subscriptionService, entityIntegrationMappingService, db),
		Coupon:                   v1.NewCouponHandler(couponService, logger),
		Addon:                    v1.NewAddonHandler(addonService, entitlementService, logger),
		Settings:                 v1.NewSettingsHandler(settingsService, logger),
		SetupIntent:              v1.NewSetupIntentHandler(integrationFactory, customerService, logger),
		Group:                    v1.NewGroupHandler(groupService, logger),
		ScheduledTask:            v1.NewScheduledTaskHandler(scheduledTaskService, logger),
		AlertLogsHandler:         v1.NewAlertLogsHandler(alertLogsService, customerService, walletService, featureService, logger),
		RBAC:                     v1.NewRBACHandler(rbacService, userService, logger),
		OAuth:                    v1.NewOAuthHandler(oauthService, cfg.OAuth.RedirectURI, logger),
		CronKafkaLagMonitoring:   cron.NewKafkaLagMonitoringHandler(logger, eventService),
		CustomerPortal:           v1.NewCustomerPortalHandler(customerPortalService, logger),
		Dashboard:                v1.NewDashboardHandler(dashboardService, logger),
	}
}

func provideRouter(handlers api.Handlers, cfg *config.Configuration, logger *logger.Logger, secretService service.SecretService, envAccessService service.EnvAccessService, rbacService *rbac.RBACService) *gin.Engine {
	return api.NewRouter(handlers, cfg, logger, secretService, envAccessService, rbacService)
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

func provideTemporalService(temporalClient client.TemporalClient, workerManager worker.TemporalWorkerManager, log *logger.Logger, sentryService *sentry.Service) temporalservice.TemporalService {
	// Initialize the global Temporal service instance with Sentry
	temporalservice.InitializeGlobalTemporalService(temporalClient, workerManager, log, sentryService)

	// Get the global instance and start it
	service := temporalservice.GetGlobalTemporalService()
	if err := service.Start(context.Background()); err != nil {
		log.Error("Failed to start global Temporal service", "error", err)
		return nil
	}

	return service
}

func startServer(
	lc fx.Lifecycle,
	cfg *config.Configuration,
	r *gin.Engine,
	consumer kafka.MessageConsumer,
	temporalClient client.TemporalClient,
	temporalService temporalservice.TemporalService,
	webhookService *webhook.WebhookService,
	router *pubsubRouter.Router,
	onboardingService service.OnboardingService,
	log *logger.Logger,
	eventPostProcessingSvc service.EventPostProcessingService,
	eventConsumptionSvc service.EventConsumptionService,
	featureUsageSvc service.FeatureUsageTrackingService,
	costSheetUsageSvc service.CostSheetUsageTrackingService,
	walletBalanceAlertSvc service.WalletBalanceAlertService,
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

		// Register all handlers and start router once
		registerRouterHandlers(router, webhookService, onboardingService, eventPostProcessingSvc, eventConsumptionSvc, featureUsageSvc, costSheetUsageSvc, walletBalanceAlertSvc, cfg, true)
		startRouter(lc, router, log)
		startTemporalWorker(lc, temporalService, params)
	case types.ModeAPI:
		startAPIServer(lc, r, cfg, log)

		// Register all handlers and start router once (no event consumption)
		registerRouterHandlers(router, webhookService, onboardingService, eventPostProcessingSvc, eventConsumptionSvc, featureUsageSvc, costSheetUsageSvc, walletBalanceAlertSvc, cfg, false)
		startRouter(lc, router, log)

	case types.ModeTemporalWorker:
		startTemporalWorker(lc, temporalService, params)
	case types.ModeConsumer:
		if consumer == nil {
			log.Fatal("Kafka consumer required for consumer mode")
		}

		// Register all handlers and start router once
		registerRouterHandlers(router, webhookService, onboardingService, eventPostProcessingSvc, eventConsumptionSvc, featureUsageSvc, costSheetUsageSvc, walletBalanceAlertSvc, cfg, true)
		startRouter(lc, router, log)
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

func registerRouterHandlers(
	router *pubsubRouter.Router,
	webhookService *webhook.WebhookService,
	onboardingService service.OnboardingService,
	eventPostProcessingSvc service.EventPostProcessingService,
	eventConsumptionSvc service.EventConsumptionService,
	featureUsageSvc service.FeatureUsageTrackingService,
	costSheetUsageSvc service.CostSheetUsageTrackingService,
	walletBalanceAlertSvc service.WalletBalanceAlertService,
	cfg *config.Configuration,
	includeProcessingHandlers bool,
) {
	webhookService.RegisterHandler(router)
	onboardingService.RegisterHandler(router)

	if includeProcessingHandlers {
		eventConsumptionSvc.RegisterHandler(router, cfg)
		eventConsumptionSvc.RegisterHandlerLazy(router, cfg)
		// eventPostProcessingSvc.RegisterHandler(router, cfg)
		featureUsageSvc.RegisterHandler(router, cfg)
		featureUsageSvc.RegisterHandlerLazy(router, cfg)
		featureUsageSvc.RegisterHandlerReplay(router, cfg)
		costSheetUsageSvc.RegisterHandler(router, cfg)
		costSheetUsageSvc.RegisterHandlerLazy(router, cfg)
		walletBalanceAlertSvc.RegisterHandler(router, cfg)
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

func provideWalletBalanceAlertPubSub(
	cfg *config.Configuration,
	logger *logger.Logger,
) types.WalletBalanceAlertPubSub {
	pubSub, err := kafkaPubsub.NewPubSubFromConfig(
		cfg,
		logger,
		cfg.WalletBalanceAlert.ConsumerGroup,
	)
	if err != nil {
		logger.Fatalw("failed to create pubsub for wallet alerts", "error", err)
		return types.WalletBalanceAlertPubSub{}
	}
	return types.WalletBalanceAlertPubSub{PubSub: pubSub}
}
