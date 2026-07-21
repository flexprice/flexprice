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
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/httpclient"
	"github.com/flexprice/flexprice/internal/integration/awsmarketplace"
	integrationevents "github.com/flexprice/flexprice/internal/integration/events"
	"github.com/flexprice/flexprice/internal/integration/gcpmarketplace"
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
	"github.com/flexprice/flexprice/internal/svix"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/temporal/queries"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/temporal/worker"
	"github.com/flexprice/flexprice/internal/tracing"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/typst"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/flexprice/flexprice/internal/webhook"
	"go.uber.org/fx"

	_ "github.com/flexprice/flexprice/docs/swagger"
	"github.com/flexprice/flexprice/internal/domain/incomingwebhookevent"
	"github.com/flexprice/flexprice/internal/domain/proration"
	syncExport "github.com/flexprice/flexprice/internal/ee/service/sync/export"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/security"
	"github.com/gin-gonic/gin"
	"github.com/nedpals/supabase-go"
)

// @title Flexprice API
// @version 1.0
// @description Flexprice API provides billing, metering, and subscription management for SaaS and usage-based products. Use it to manage customers, plans, invoices, payments, usage events, and entitlements. Authenticate with an API key in the x-api-key header.
// @contact.name API Support
// @license.name AGPL-3.0
// @license.url https://www.gnu.org/licenses/agpl-3.0.html
// @description Flexprice API Service
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

			// Config — validated at boot (fail-fast for non-local deployments)
			config.NewValidatedConfig,

			// Logger
			logger.NewLogger,

			// Security
			security.NewEncryptionService,

			// Marketplace clients
			awsmarketplace.NewClient,
			gcpmarketplace.NewClient,

			// RBAC
			rbac.NewRBACService,

			// storage
			s3.NewService,

			// Monitoring
			tracing.NewService,
			pyroscope.NewPyroscopeService,

			// Cache
			cache.InitializeInMemoryCache,
			cache.NewRedisCache,
			cache.NewRedisLocker,

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
			kafka.NewSecondaryProducer,
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
			repository.NewCostSheetUsageRepository,
			repository.NewMeterUsageRepository,
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
			repository.NewInvoiceLineItemRepository,
			repository.NewFeatureRepository,
			repository.NewEntitlementRepository,
			repository.NewPaymentRepository,
			repository.NewPaymentMethodRepository,
			repository.NewRefundRepository,
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
			repository.NewUsageRecordRepository,
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
			repository.NewAlertSettingsRepository,
			repository.NewIncomingWebhookEventRepository,
			repository.NewSystemEventRepository,
			repository.NewSystemEventDomainRepository,
			repository.NewGroupRepository,
			repository.NewScheduledTaskRepository,
			repository.NewPriceUnitRepository,
			repository.NewWorkflowExecutionRepository,
			repository.NewCheckoutSessionRepository,
			repository.NewRawEventRepository,

			// PubSub
			pubsubRouter.NewRouter,

			// Proration
			proration.NewCalculator,
		),
	)

	// Webhook module (must be initialised before services)
	opts = append(opts, webhook.Module)

	// Integration events module — isolated consumer group on system_events topic (webhook-shaped events).
	opts = append(opts, integrationevents.Module)

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
			provideSupabaseClient,
			service.NewUserService,
			service.NewEnvAccessService,
			service.NewEnvironmentService,
			service.NewMeterService,
			service.NewEventService,
			service.NewEventConsumptionService,
			service.NewRawEventsReprocessingService,
			service.NewRawEventConsumptionService,
			service.NewCostSheetUsageTrackingService,
			service.NewMeterUsageTrackingService,
			service.NewMeterUsageService,
			service.NewCheckoutSessionService,
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
			service.NewGeminiPricingService,
			service.NewBillingService,
			service.NewCreditGrantService,
			service.NewCostsheetService,
			service.NewRevenueAnalyticsService,
			service.NewCreditNoteService,
			service.NewConnectionService,
			service.NewMarketplaceService,
			service.NewEntityIntegrationMappingService,
			service.NewIntegrationSyncService,
			service.NewTaxService,
			service.NewCouponService,
			service.NewCouponAssociationService,
			service.NewAddonService,
			service.NewSettingsService,
			service.NewSubscriptionChangeService,
			service.NewSubscriptionModificationService,
			service.NewSubscriptionScheduleService,
			service.NewAlertLogsService,
			service.NewAlertService,
			service.NewGroupService,
			service.NewScheduledTaskService,
			service.NewWalletPaymentService,
			service.NewWalletBalanceAlertService,
			service.NewCustomerPortalService,
			service.NewDashboardService,
			service.NewWorkflowExecutionService,
			service.NewWorkflowService,
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
			provideWorkflowQuerier,

			// API components
			provideHandlers,
			provideRouter,
		),
		fx.Invoke(
			tracing.RegisterHooks,
			repository.InitTracing,
			pyroscope.RegisterHooks,
			initIntegrationFactory,
			startServer,
		),
	)
	app := fx.New(opts...)
	app.Run()
}

func provideHandlers(
	cfg *config.Configuration,
	logger *logger.Logger,
	redisCache cache.RedisCache,
	locker cache.Locker,
	meterService service.MeterService,
	eventService service.EventService,
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
	marketplaceService service.MarketplaceService,
	entityIntegrationMappingService service.EntityIntegrationMappingService,
	integrationSyncService service.IntegrationSyncService,
	svixClient *svix.Client,
	taxService service.TaxService,
	couponService service.CouponService,
	couponAssociationService service.CouponAssociationService,
	addonService service.AddonService,
	settingsService service.SettingsService,
	subscriptionChangeService service.SubscriptionChangeService,
	subscriptionModificationService service.SubscriptionModificationService,
	subscriptionScheduleService service.SubscriptionScheduleService,
	rawEventsReprocessingService service.RawEventsReprocessingService,
	rawEventConsumptionService service.RawEventConsumptionService,
	alertLogsService service.AlertLogsService,
	alertService service.AlertService,
	groupService service.GroupService,
	integrationFactory *integration.Factory,
	db postgres.IClient,
	scheduledTaskService service.ScheduledTaskService,
	rbacService *rbac.RBACService,
	oauthService service.OAuthService,
	costsheetUsageTrackingService service.CostSheetUsageTrackingService,
	customerPortalService service.CustomerPortalService,
	dashboardService service.DashboardService,
	workflowService service.WorkflowService,
	meterUsageService service.MeterUsageService,
	checkoutSessionService service.CheckoutSessionService,
	geminiPricingService service.GeminiPricingService,
	webhookService *webhook.WebhookService,
) api.Handlers {
	return api.Handlers{
		Events:                   v1.NewEventsHandler(eventService, rawEventsReprocessingService, rawEventConsumptionService, meterUsageService, cfg, logger),
		Meter:                    v1.NewMeterHandler(meterService, logger),
		Auth:                     v1.NewAuthHandler(cfg, authService, logger),
		User:                     v1.NewUserHandler(userService, logger),
		Environment:              v1.NewEnvironmentHandler(environmentService, logger),
		Health:                   v1.NewHealthHandler(logger),
		Price:                    v1.NewPriceHandler(priceService, logger),
		PriceUnit:                v1.NewPriceUnitHandler(priceUnitService, logger),
		Customer:                 v1.NewCustomerHandler(customerService, billingService, entityIntegrationMappingService, logger),
		Plan:                     v1.NewPlanHandler(planService, entitlementService, creditGrantService, temporalService, locker, cfg, logger),
		Subscription:             v1.NewSubscriptionHandler(subscriptionService, logger),
		SubscriptionChange:       v1.NewSubscriptionChangeHandler(subscriptionChangeService, logger),
		SubscriptionModification: v1.NewSubscriptionModificationHandler(subscriptionModificationService, logger),
		SubscriptionSchedule:     v1.NewSubscriptionScheduleHandler(subscriptionScheduleService),
		Wallet:                   v1.NewWalletHandler(walletService, logger),
		Tenant:                   v1.NewTenantHandler(tenantService, logger),
		Invoice:                  v1.NewInvoiceHandler(invoiceService, cfg, logger),
		Feature:                  v1.NewFeatureHandler(featureService, logger),
		Entitlement:              v1.NewEntitlementHandler(entitlementService, logger),
		Payment:                  v1.NewPaymentHandler(paymentService, paymentProcessorService, logger),
		Task:                     v1.NewTaskHandler(taskService, temporalService, logger),
		Secret:                   v1.NewSecretHandler(secretService, logger),
		Tax:                      v1.NewTaxHandler(taxService, logger),
		Onboarding:               v1.NewOnboardingHandler(onboardingService, logger),
		AIPricing:                v1.NewAIPricingHandler(geminiPricingService, logger),
		CronSubscription:         cron.NewSubscriptionHandler(subscriptionService, logger),
		CronWallet:               cron.NewWalletCronHandler(logger, walletService, tenantService, environmentService, featureService, alertLogsService),
		CronInvoice:              cron.NewInvoiceHandler(invoiceService, subscriptionService, connectionService, tenantService, environmentService, integrationFactory, logger),
		CreditGrant:              v1.NewCreditGrantHandler(creditGrantService, logger),
		Costsheet:                v1.NewCostsheetHandler(costsheetService, logger),
		RevenueAnalytics:         v1.NewRevenueAnalyticsHandler(revenueAnalyticsService, costsheetUsageTrackingService, cfg, logger),
		CronCreditGrant:          cron.NewCreditGrantCronHandler(creditGrantService, logger),
		CreditNote:               v1.NewCreditNoteHandler(creditNoteService, logger),
		Connection:               v1.NewConnectionHandler(connectionService, logger),
		Marketplace:              v1.NewMarketplaceHandler(marketplaceService, logger),
		Integration:              v1.NewIntegrationHandler(integrationSyncService, entityIntegrationMappingService, connectionService, logger),
		Paddle:                   v1.NewPaddleHandler(integrationFactory, logger),
		Webhook:                  v1.NewWebhookHandler(cfg, svixClient, logger, integrationFactory, customerService, paymentService, invoiceService, planService, subscriptionService, entityIntegrationMappingService, checkoutSessionService, db, webhookService),
		Coupon:                   v1.NewCouponHandler(couponService, couponAssociationService, logger),
		Addon:                    v1.NewAddonHandler(addonService, entitlementService, logger),
		Settings:                 v1.NewSettingsHandler(settingsService, logger),
		SetupIntent:              v1.NewSetupIntentHandler(integrationFactory, customerService, cfg, logger),
		Group:                    v1.NewGroupHandler(groupService, logger),
		ScheduledTask:            v1.NewScheduledTaskHandler(scheduledTaskService, logger),
		AlertLogsHandler:         v1.NewAlertLogsHandler(alertLogsService, customerService, walletService, featureService, logger),
		AlertSettingsHandler:     v1.NewAlertSettingsHandler(alertService, logger),
		RBAC:                     v1.NewRBACHandler(rbacService, userService, logger),
		OAuth:                    v1.NewOAuthHandler(oauthService, cfg.OAuth.RedirectURI, logger),
		CronKafkaLagMonitoring:   cron.NewKafkaLagMonitoringHandler(logger, eventService),
		CustomerPortal:           v1.NewCustomerPortalHandler(customerPortalService, logger),
		Dashboard:                v1.NewDashboardHandler(dashboardService, logger),
		Workflow:                 v1.NewWorkflowHandler(workflowService, logger),
		MeterUsage:               v1.NewMeterUsageHandler(meterUsageService, logger),
		CheckoutSession:          v1.NewCheckoutSessionHandler(checkoutSessionService, logger),
	}
}

func provideRouter(
	handlers api.Handlers,
	cfg *config.Configuration,
	logger *logger.Logger,
	secretService service.SecretService,
	envAccessService service.EnvAccessService,
	rbacService *rbac.RBACService,
	tenantService service.TenantService,
	webhookRequestRepo incomingwebhookevent.Repository,
) *gin.Engine {
	return api.NewRouter(
		handlers,
		cfg,
		logger,
		secretService,
		envAccessService,
		rbacService,
		tenantService,
		webhookRequestRepo,
	)
}

func initIntegrationFactory(factory *integration.Factory, paymentService interfaces.PaymentService, invoiceService service.InvoiceService) {
	factory.SetServices(paymentService, invoiceService)
}

func provideSupabaseClient(cfg *config.Configuration) *supabase.Client {
	if cfg == nil || cfg.Auth.Supabase.BaseURL == "" || cfg.Auth.Supabase.ServiceKey == "" {
		return nil
	}
	return supabase.CreateClient(cfg.Auth.Supabase.BaseURL, cfg.Auth.Supabase.ServiceKey)
}

func provideTemporalConfig(cfg *config.Configuration) *config.TemporalConfig {
	return &cfg.Temporal
}

func provideTemporalClient(cfg *config.TemporalConfig, log *logger.Logger) (client.TemporalClient, error) {
	log.Info(context.Background(), "Initializing Temporal client", "address", cfg.Address, "namespace", cfg.Namespace)

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
		log.Error(context.Background(), "Failed to create Temporal client", "error", err)
		return nil, fmt.Errorf("failed to create temporal client: %w", err)
	}

	log.Info(context.Background(), "Temporal client created successfully")
	return temporalClient, nil
}

func provideTemporalWorkerManager(temporalClient client.TemporalClient, log *logger.Logger) worker.TemporalWorkerManager {
	return worker.NewTemporalWorkerManager(temporalClient, log)
}

func provideTemporalService(temporalClient client.TemporalClient, workerManager worker.TemporalWorkerManager, log *logger.Logger, tracingSvc *tracing.Service, cfg *config.TemporalConfig) temporalservice.TemporalService {
	// Initialize the global Temporal service instance with tracing
	temporalservice.InitializeGlobalTemporalService(temporalClient, workerManager, log, tracingSvc, cfg)

	// Get the global instance and start it
	service := temporalservice.GetGlobalTemporalService()
	if err := service.Start(context.Background()); err != nil {
		log.Error(context.Background(), "Failed to start global Temporal service", "error", err)
		return nil
	}

	return service
}

func provideWorkflowQuerier(temporalClient client.TemporalClient, log *logger.Logger) *queries.WorkflowQuerier {
	return queries.NewWorkflowQuerier(temporalClient.GetRawClient(), log)
}

func startServer(
	lc fx.Lifecycle,
	cfg *config.Configuration,
	r *gin.Engine,
	consumer kafka.MessageConsumer,
	temporalClient client.TemporalClient,
	temporalService temporalservice.TemporalService,
	webhookService *webhook.WebhookService,
	integrationEventService *integrationevents.IntegrationEventService,
	router *pubsubRouter.Router,
	onboardingService service.OnboardingService,
	log *logger.Logger,
	eventConsumptionSvc service.EventConsumptionService,
	costSheetUsageSvc service.CostSheetUsageTrackingService,
	walletBalanceAlertSvc service.WalletBalanceAlertService,
	rawEventConsumptionSvc service.RawEventConsumptionService,
	meterUsageTrackingSvc service.MeterUsageTrackingService,
	params service.ServiceParams,
) {
	mode := cfg.Deployment.Mode
	if mode == "" {
		mode = types.ModeLocal
	}

	switch mode {
	case types.ModeLocal:
		if consumer == nil {
			log.Fatal(context.Background(), "Kafka consumer required for local mode")
		}
		startAPIServer(lc, r, cfg, log)

		// Register all handlers and start router once
		registerRouterHandlers(router, webhookService, integrationEventService, onboardingService, eventConsumptionSvc, costSheetUsageSvc, walletBalanceAlertSvc, rawEventConsumptionSvc, meterUsageTrackingSvc, cfg, true)
		startRouter(lc, router, log)
		startTemporalWorker(lc, log, temporalClient, temporalService, params, webhookService)
	case types.ModeAPI:
		startAPIServer(lc, r, cfg, log)

		// Register all handlers and start router once (no event consumption)
		registerRouterHandlers(router, webhookService, integrationEventService, onboardingService, eventConsumptionSvc, costSheetUsageSvc, walletBalanceAlertSvc, rawEventConsumptionSvc, meterUsageTrackingSvc, cfg, false)
		startRouter(lc, router, log)

	case types.ModeTemporalWorker:
		// Register webhook handler and start router so that webhook events
		// published by temporal activities (e.g. invoice finalization) are
		// consumed and delivered via Svix/native in the same process.
		registerRouterHandlers(router, webhookService, integrationEventService, onboardingService, eventConsumptionSvc, costSheetUsageSvc, walletBalanceAlertSvc, rawEventConsumptionSvc, meterUsageTrackingSvc, cfg, false)
		startRouter(lc, router, log)
		startTemporalWorker(lc, log, temporalClient, temporalService, params, webhookService)
	case types.ModeConsumer:
		if consumer == nil {
			log.Fatal(context.Background(), "Kafka consumer required for consumer mode")
		}

		// Register all handlers and start router once
		registerRouterHandlers(router, webhookService, integrationEventService, onboardingService, eventConsumptionSvc, costSheetUsageSvc, walletBalanceAlertSvc, rawEventConsumptionSvc, meterUsageTrackingSvc, cfg, true)
		startRouter(lc, router, log)
	default:
		log.Fatalf("Unknown deployment mode: %s", mode)
	}
}

func startTemporalWorker(
	lc fx.Lifecycle,
	log *logger.Logger,
	temporalClient client.TemporalClient,
	temporalService temporalservice.TemporalService,
	params service.ServiceParams,
	webhookService *webhook.WebhookService,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := temporalservice.EnsureSchedules(ctx, temporalClient, log); err != nil {
				return fmt.Errorf("ensure temporal server schedules: %w", err)
			}

			// Register workflows and activities first (this creates the workers)
			if err := temporal.RegisterWorkflowsAndActivities(temporalService, params, webhookService); err != nil {
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
	log.Info(context.Background(), "Registering API server start hook")
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			log.Info(ctx, "Starting API server...")
			go func() {
				if err := r.Run(cfg.Server.Address); err != nil {
					log.Fatalf("Failed to start server: %v", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			log.Info(ctx, "Shutting down server...")
			log.Shutdown(ctx)
			return nil
		},
	})
}

func registerRouterHandlers(
	router *pubsubRouter.Router,
	webhookService *webhook.WebhookService,
	integrationEventService *integrationevents.IntegrationEventService,
	onboardingService service.OnboardingService,
	eventConsumptionSvc service.EventConsumptionService,
	costSheetUsageSvc service.CostSheetUsageTrackingService,
	walletBalanceAlertSvc service.WalletBalanceAlertService,
	rawEventConsumptionSvc service.RawEventConsumptionService,
	meterUsageTrackingSvc service.MeterUsageTrackingService,
	cfg *config.Configuration,
	includeProcessingHandlers bool,
) {
	if includeProcessingHandlers {
		onboardingService.RegisterHandler(router, cfg)
		webhookService.RegisterHandler(router)
		integrationEventService.RegisterHandler(router)
		eventConsumptionSvc.RegisterHandler(router, cfg)
		eventConsumptionSvc.RegisterHandlerLazy(router, cfg)
		eventConsumptionSvc.RegisterHandlerReplay(router, cfg)
		costSheetUsageSvc.RegisterHandler(router, cfg)
		costSheetUsageSvc.RegisterHandlerLazy(router, cfg)
		walletBalanceAlertSvc.RegisterHandler(router, cfg)
		rawEventConsumptionSvc.RegisterHandler(router, cfg)
		meterUsageTrackingSvc.RegisterHandler(router, cfg)
		meterUsageTrackingSvc.RegisterHandlerLazy(router, cfg)
	}
}

func startRouter(
	lc fx.Lifecycle,
	router *pubsubRouter.Router,
	logger *logger.Logger,
) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			logger.Info(ctx, "starting message router")
			go func() {
				if err := router.Run(); err != nil {
					logger.Errorw("message router failed", "error", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			logger.Info(ctx, "stopping message router")
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
