package api

import (
	"github.com/flexprice/flexprice/docs/swagger"
	"github.com/flexprice/flexprice/internal/api/cron"
	v1 "github.com/flexprice/flexprice/internal/api/v1"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type Handlers struct {
	Events                   *v1.EventsHandler
	Meter                    *v1.MeterHandler
	Auth                     *v1.AuthHandler
	User                     *v1.UserHandler
	Environment              *v1.EnvironmentHandler
	Health                   *v1.HealthHandler
	Price                    *v1.PriceHandler
	PriceUnit                *v1.PriceUnitHandler
	Customer                 *v1.CustomerHandler
	Connection               *v1.ConnectionHandler
	Plan                     *v1.PlanHandler
	Subscription             *v1.SubscriptionHandler
	SubscriptionChange       *v1.SubscriptionChangeHandler
	SubscriptionModification *v1.SubscriptionModificationHandler
	SubscriptionSchedule     *v1.SubscriptionScheduleHandler
	Wallet                   *v1.WalletHandler
	Tenant                   *v1.TenantHandler
	Invoice                  *v1.InvoiceHandler
	Feature                  *v1.FeatureHandler
	Entitlement              *v1.EntitlementHandler
	CreditGrant              *v1.CreditGrantHandler
	Payment                  *v1.PaymentHandler
	Task                     *v1.TaskHandler
	Secret                   *v1.SecretHandler
	Costsheet                *v1.CostsheetHandler
	RevenueAnalytics         *v1.RevenueAnalyticsHandler
	CreditNote               *v1.CreditNoteHandler
	Tax                      *v1.TaxHandler
	Coupon                   *v1.CouponHandler
	Webhook                  *v1.WebhookHandler
	Addon                    *v1.AddonHandler
	Integration              *v1.IntegrationHandler
	Paddle                   *v1.PaddleHandler
	Settings                 *v1.SettingsHandler
	SetupIntent              *v1.SetupIntentHandler
	Group                    *v1.GroupHandler
	ScheduledTask            *v1.ScheduledTaskHandler
	AlertLogsHandler         *v1.AlertLogsHandler
	RBAC                     *v1.RBACHandler
	OAuth                    *v1.OAuthHandler
	Dashboard                *v1.DashboardHandler
	Workflow                 *v1.WorkflowHandler
	MeterUsage               *v1.MeterUsageHandler

	// Portal handlers
	Onboarding     *v1.OnboardingHandler
	AIPricing      *v1.AIPricingHandler
	CustomerPortal *v1.CustomerPortalHandler
	// Cron jobs: optional HTTP /v1/cron/... manual triggers; same work is automated via Temporal server schedules (worker creates them on startup).
	CronSubscription       *cron.SubscriptionHandler
	CronWallet             *cron.WalletCronHandler
	CronCreditGrant        *cron.CreditGrantCronHandler
	CronInvoice            *cron.InvoiceHandler
	CronKafkaLagMonitoring *cron.KafkaLagMonitoringHandler
}

func NewRouter(
	handlers Handlers,
	cfg *config.Configuration,
	logger *logger.Logger,
	secretService service.SecretService,
	envAccessService service.EnvAccessService,
	rbacService *rbac.RBACService,
	tenantService service.TenantService,
) *gin.Engine {
	// gin.SetMode(gin.ReleaseMode)

	// Create a new gin engine without default middleware
	router := gin.New()

	// Add recovery middleware (panic recovery)
	router.Use(gin.RecoveryWithWriter(logger.GetGinLogger()))

	// Add our custom middleware in order
	router.Use(
		middleware.RequestIDMiddleware,       // Generate/extract request ID first
		middleware.LoggingMiddleware(logger), // Use our standard logger for HTTP logging
		middleware.CORSMiddleware,
	)
	// Tracing middleware fans out into otelgin (SigNoz / OTLP) and sentrygin
	// (panic recovery + Sentry scope binding). Each handler is added separately
	// because gin's Use signature is variadic and the slice may be empty.
	for _, h := range middleware.TracingMiddleware(cfg) {
		router.Use(h)
	}
	router.Use(middleware.PyroscopeMiddleware(cfg)) // Add Pyroscope middleware

	// Initialize permission middleware
	permissionMW := middleware.NewPermissionMiddleware(rbacService, logger)
	write := permissionMW.RequirePermission // shorthand used on every write route

	// Add middleware to set swagger host dynamically
	router.Use(func(c *gin.Context) {
		if swagger.SwaggerInfo != nil {
			swagger.SwaggerInfo.Host = c.Request.Host
		}
		c.Next()
	})

	// Health check
	router.GET("/health", handlers.Health.Health)
	router.POST("/health", handlers.Health.Health)

	// Public routes
	public := router.Group("/", middleware.GuestAuthenticateMiddleware)

	v1Public := public.Group("/v1")
	v1Public.Use(middleware.ErrorHandler())

	{
		// Auth routes
		v1Public.POST("/auth/signup", handlers.Auth.SignUp)
		v1Public.POST("/auth/login", handlers.Auth.Login)
	}

	private := router.Group("/", middleware.AuthenticateMiddleware(cfg, secretService, logger))
	private.Use(middleware.TenantStatusMiddleware(tenantService, logger))
	private.Use(middleware.EnvAccessMiddleware(envAccessService, logger))
	private.Use(middleware.TenantContextMiddleware)

	v1Private := private.Group("/v1")
	v1Private.Use(middleware.ErrorHandler())
	{
		user := v1Private.Group("/users")
		{
			user.GET("/me", handlers.User.GetUserInfo)
			user.POST("", write("user", types.ActionWrite), handlers.User.CreateUser)
			user.PUT("/me", write("user", types.ActionWrite), handlers.User.UpdateUser)
			user.POST("/search", handlers.User.QueryUsers)
		}

		environment := v1Private.Group("/environments")
		{
			environment.POST("", write("environment", types.ActionWrite), handlers.Environment.CreateEnvironment)
			environment.GET("", handlers.Environment.GetEnvironments)
			environment.GET("/:id", handlers.Environment.GetEnvironment)
			environment.PUT("/:id", write("environment", types.ActionWrite), handlers.Environment.UpdateEnvironment)
			environment.POST("/:id/clone", write("environment", types.ActionWrite), handlers.Environment.CloneEnvironment)
		}

		// Events routes
		events := v1Private.Group("/events")
		{
			events.POST("", write("event", types.ActionWrite), handlers.Events.IngestEvent)
			events.POST("/bulk", write("event", types.ActionWrite), handlers.Events.BulkIngestEvent)
			events.GET("", handlers.Events.GetEvents)
			events.GET("/:id", handlers.Events.GetEventByID)
			events.POST("/query", handlers.Events.QueryEvents)
			events.POST("/usage", handlers.Events.GetUsage)
			events.POST("/usage/meter", handlers.Events.GetUsageByMeter)
			events.POST("/analytics", handlers.Events.GetUsageAnalytics)
			events.POST("/analytics-v2", handlers.Events.GetUsageAnalyticsV2)
			events.POST("/huggingface-billing", handlers.Events.GetHuggingFaceBillingData)
			events.GET("/monitoring", handlers.Events.GetMonitoringData)
			events.POST("/reprocess", write("event", types.ActionWrite), handlers.Events.ReprocessEvents)
			events.POST("/raw/bulk", write("event", types.ActionWrite), handlers.Events.BulkIngestRawEvent)
			events.POST("/raw/reprocess/all", handlers.Events.ReprocessRawEvents)
			events.POST("/raw/reprocess/pending", handlers.Events.ReprocessUnprocessedRawEvents)
			events.POST("/reprocess/internal", write("event", types.ActionWrite), handlers.Events.ReprocessEventsInternal)
		}

		// Meter usage query endpoints (reads from meter_usage ClickHouse table)
		meterUsage := v1Private.Group("/meter-usage")
		{
			meterUsage.POST("/query", handlers.MeterUsage.QueryUsage)
			meterUsage.POST("/analytics", handlers.MeterUsage.GetAnalytics)
			meterUsage.POST("/detailed-analytics", handlers.MeterUsage.GetDetailedAnalytics)
		}

		meters := v1Private.Group("/meters")
		{
			meters.POST("", write("meter", types.ActionWrite), handlers.Meter.CreateMeter)
			meters.GET("", handlers.Meter.GetAllMeters)
			meters.GET("/:id", handlers.Meter.GetMeter)
			meters.POST("/:id/disable", write("meter", types.ActionWrite), handlers.Meter.DisableMeter)
			meters.DELETE("/:id", write("meter", types.ActionWrite), handlers.Meter.DeleteMeter)
			meters.PUT("/:id", write("meter", types.ActionWrite), handlers.Meter.UpdateMeter)
		}

		price := v1Private.Group("/prices")
		{
			price.POST("", write("price", types.ActionWrite), handlers.Price.CreatePrice)
			price.POST("/bulk", write("price", types.ActionWrite), handlers.Price.CreateBulkPrice)
			price.GET("", handlers.Price.ListPrices)
			price.GET("/:id", handlers.Price.GetPrice)
			price.PUT("/:id", write("price", types.ActionWrite), handlers.Price.UpdatePrice)
			price.DELETE("/:id", write("price", types.ActionWrite), handlers.Price.DeletePrice)
			price.GET("/lookup/:lookup_key", handlers.Price.GetByLookupKey)
			price.POST("/search", handlers.Price.QueryPrices)

			priceUnit := price.Group("/units")
			{
				priceUnit.POST("", write("price", types.ActionWrite), handlers.PriceUnit.CreatePriceUnit)
				priceUnit.GET("", handlers.PriceUnit.ListPriceUnits)
				priceUnit.GET("/:id", handlers.PriceUnit.GetPriceUnit)
				priceUnit.GET("/code/:code", handlers.PriceUnit.GetPriceUnitByCode)
				priceUnit.PUT("/:id", write("price", types.ActionWrite), handlers.PriceUnit.UpdatePriceUnit)
				priceUnit.DELETE("/:id", write("price", types.ActionWrite), handlers.PriceUnit.DeletePriceUnit)
				priceUnit.POST("/search", handlers.PriceUnit.QueryPriceUnits)
			}
		}

		customer := v1Private.Group("/customers")
		{
			// list customers by filter
			customer.POST("/search", handlers.Customer.QueryCustomers)

			customer.POST("", write("customer", types.ActionWrite), handlers.Customer.CreateCustomer)
			customer.GET("", handlers.Customer.ListCustomers)
			customer.PUT("", write("customer", types.ActionWrite), handlers.Customer.UpdateCustomer)
			customer.GET("/:id", handlers.Customer.GetCustomer)
			customer.PUT("/:id", write("customer", types.ActionWrite), handlers.Customer.UpdateCustomer)
			customer.DELETE("/:id", write("customer", types.ActionWrite), handlers.Customer.DeleteCustomer)
			customer.GET("/lookup/:lookup_key", handlers.Customer.GetCustomerByLookupKey)
			customer.GET("/external/:external_id", handlers.Customer.GetCustomerByLookupKey)

			// New endpoints for entitlements and usage
			customer.GET("/:id/entitlements", handlers.Customer.GetCustomerEntitlements)
			customer.GET("/usage", handlers.Customer.GetCustomerUsageSummary)
			customer.GET("/:id/usage", handlers.Customer.GetCustomerUsageSummary)
			customer.GET("/:id/grants/upcoming", handlers.Customer.GetUpcomingCreditGrantApplications)

			// other routes for customer
			customer.GET("/:id/wallets", handlers.Wallet.GetWalletsByCustomerID)
			customer.GET("/:id/invoices/summary", handlers.Invoice.GetCustomerInvoiceSummary)
			customer.GET("/wallets", handlers.Wallet.GetCustomerWallets)

			// Customer Dashboard - Session creation (requires tenant auth)
			customer.GET("/portal/:external_id", handlers.CustomerPortal.CreateSession)
		}

		plan := v1Private.Group("/plans")
		{
			// list plans by filter
			plan.POST("/search", handlers.Plan.QueryPlans)

			plan.POST("", write("plan", types.ActionWrite), handlers.Plan.CreatePlan)
			plan.GET("", handlers.Plan.ListPlans)
			plan.GET("/:id", handlers.Plan.GetPlan)
			plan.PUT("/:id", write("plan", types.ActionWrite), handlers.Plan.UpdatePlan)
			plan.DELETE("/:id", write("plan", types.ActionWrite), handlers.Plan.DeletePlan)
			plan.POST("/:id/clone", write("plan", types.ActionWrite), handlers.Plan.ClonePlan)
			plan.POST("/:id/sync/subscriptions", write("plan", types.ActionWrite), handlers.Plan.SyncPlanPrices)
			plan.POST("/:id/sync/subscriptions/v2", write("plan", types.ActionWrite), handlers.Plan.SyncPlanPricesV2)

			// entitlement routes
			plan.GET("/:id/entitlements", handlers.Plan.GetPlanEntitlements)
			plan.GET("/:id/creditgrants", handlers.Plan.GetPlanCreditGrants)
		}

		addon := v1Private.Group("/addons")
		{
			// list addons by filter
			addon.POST("/search", handlers.Addon.QueryAddons)

			addon.POST("", write("addon", types.ActionWrite), handlers.Addon.CreateAddon)
			addon.GET("", handlers.Addon.ListAddons)
			addon.GET("/:id", handlers.Addon.GetAddon)
			addon.GET("/lookup/:lookup_key", handlers.Addon.GetAddonByLookupKey)
			addon.PUT("/:id", write("addon", types.ActionWrite), handlers.Addon.UpdateAddon)
			addon.GET("/:id/entitlements", handlers.Addon.GetAddonEntitlements)
			addon.DELETE("/:id", write("addon", types.ActionWrite), handlers.Addon.DeleteAddon)
		}

		group := v1Private.Group("/groups")
		{
			group.POST("", write("group", types.ActionWrite), handlers.Group.CreateGroup)
			group.POST("/search", handlers.Group.QueryGroups)
			group.GET("/:id", handlers.Group.GetGroup)
			group.DELETE("/:id", write("group", types.ActionWrite), handlers.Group.DeleteGroup)
		}

		subscription := v1Private.Group("/subscriptions")
		{
			subscription.POST("/search", handlers.Subscription.QuerySubscriptions)
			subscription.POST("", write("subscription", types.ActionWrite), handlers.Subscription.CreateSubscription)
			subscription.GET("", handlers.Subscription.ListSubscriptions)
			subscription.POST("/lineitems/search", handlers.Subscription.QuerySubscriptionLineItems)
			subscription.GET("/:id", handlers.Subscription.GetSubscription)
			subscription.PUT("/:id", write("subscription", types.ActionWrite), handlers.Subscription.UpdateSubscription)
			subscription.GET("/:id/v2", handlers.Subscription.GetSubscriptionV2)
			subscription.POST("/:id/activate", write("subscription", types.ActionWrite), handlers.Subscription.ActivateDraftSubscription)
			subscription.POST("/:id/cancel", write("subscription", types.ActionWrite), handlers.Subscription.CancelSubscription)
			subscription.POST("/usage", handlers.Subscription.GetUsageBySubscription)

			subscription.GET("/:id/entitlements", handlers.Subscription.GetSubscriptionEntitlements)
			subscription.GET("/:id/grants/upcoming", handlers.Subscription.GetUpcomingCreditGrantApplications)

			// Addon management for subscriptions - moved under subscription handler
			subscription.POST("/addon", write("subscription", types.ActionWrite), handlers.Subscription.AddAddonToSubscription)
			subscription.DELETE("/addon", write("subscription", types.ActionWrite), handlers.Subscription.RemoveAddonToSubscription)
			subscription.GET("/:id/addons/associations", handlers.Subscription.GetActiveAddonAssociations)

			// Subscription plan changes (upgrade/downgrade)
			subscription.POST("/:id/change/preview", handlers.SubscriptionChange.PreviewSubscriptionChange)
			subscription.POST("/:id/change/execute", write("subscription", types.ActionWrite), handlers.SubscriptionChange.ExecuteSubscriptionChange)
			subscription.POST(":id/modify/execute", write("subscription", types.ActionWrite), handlers.SubscriptionModification.Execute)
			subscription.POST(":id/modify/preview", handlers.SubscriptionModification.Preview)

			// Subscription line item management (POST /lineitems/search registered above)
			subscription.POST("/:id/lineitems", write("subscription", types.ActionWrite), handlers.Subscription.AddSubscriptionLineItem)
			subscription.PUT("/lineitems/:id", write("subscription", types.ActionWrite), handlers.Subscription.UpdateSubscriptionLineItem)
			subscription.DELETE("/lineitems/:id", write("subscription", types.ActionWrite), handlers.Subscription.DeleteSubscriptionLineItem)

			subscription.POST("/temporal/schedule-update-billing-period", write("subscription", types.ActionWrite), handlers.ScheduledTask.ScheduleUpdateBillingPeriod)
			subscription.POST("/temporal/schedule-draft-finalization", write("subscription", types.ActionWrite), handlers.ScheduledTask.ScheduleDraftFinalization)

			// Trigger subscription billing workflow
			subscription.POST("/temporal/:subscription_id/trigger-workflow", write("subscription", types.ActionWrite), handlers.Subscription.TriggerSubscriptionWorkflow)
			subscription.POST("/temporal/:subscription_id/draft-and-compute", write("subscription", types.ActionWrite), handlers.Subscription.TriggerSubscriptionDraftAndComputeWorkflow)

			// Subscription schedules - nested group
			subscription.GET("/:id/schedules", handlers.SubscriptionSchedule.ListSchedulesForSubscription)

			schedules := subscription.Group("/schedules")
			{
				schedules.GET("", handlers.SubscriptionSchedule.ListSchedules)
				schedules.GET("/:schedule_id", handlers.SubscriptionSchedule.GetSchedule)
				schedules.POST("/:schedule_id/cancel", write("subscription", types.ActionWrite), handlers.SubscriptionSchedule.CancelSchedule)
				schedules.POST("/cancel", write("subscription", types.ActionWrite), handlers.SubscriptionSchedule.CancelSchedule)
			}
		}

		wallet := v1Private.Group("/wallets")
		{
			wallet.POST("", write("wallet", types.ActionWrite), handlers.Wallet.CreateWallet)
			wallet.GET("", handlers.Wallet.ListWallets)
			wallet.GET("/:id", handlers.Wallet.GetWalletByID)
			wallet.GET("/:id/transactions", handlers.Wallet.GetWalletTransactions)
			wallet.POST("/:id/top-up", write("wallet", types.ActionWrite), handlers.Wallet.TopUpWallet)
			wallet.POST("/:id/terminate", write("wallet", types.ActionWrite), handlers.Wallet.TerminateWallet)
			wallet.POST("/:id/modify", write("wallet", types.ActionWrite), handlers.Wallet.ModifyWallet)
			wallet.GET("/:id/balance/real-time", handlers.Wallet.GetWalletBalance)
			wallet.GET("/:id/balance/real-time-cached", handlers.Wallet.GetWalletBalanceForceCached)
			wallet.PUT("/:id", write("wallet", types.ActionWrite), handlers.Wallet.UpdateWallet)
			wallet.POST("/:id/debit", write("wallet", types.ActionWrite), handlers.Wallet.ManualBalanceDebit)
			wallet.POST("/transactions/search", handlers.Wallet.QueryWalletTransactions)
			wallet.POST("/search", handlers.Wallet.QueryWallets)
		}

		// Tenant routes
		tenantRoutes := v1Private.Group("/tenants")
		{
			tenantRoutes.PUT("/update", write("tenant", types.ActionWrite), handlers.Tenant.UpdateTenant)
			tenantRoutes.GET("/:id", handlers.Tenant.GetTenantByID)
			tenantRoutes.GET("/billing", handlers.Tenant.GetTenantBillingUsage)
		}

		invoices := v1Private.Group("/invoices")
		{
			invoices.POST("/temporal/:invoice_id/finalize", write("invoice", types.ActionWrite), handlers.Invoice.TriggerFinalizeDraftInvoiceWorkflow)
			invoices.POST("/search", handlers.Invoice.QueryInvoices)
			invoices.POST("", write("invoice", types.ActionWrite), handlers.Invoice.CreateOneOffInvoice)
			invoices.GET("", handlers.Invoice.ListInvoices)
			invoices.GET("/:id", handlers.Invoice.GetInvoice)
			invoices.PUT("/:id", write("invoice", types.ActionWrite), handlers.Invoice.UpdateInvoice)
			invoices.POST("/:id/finalize", write("invoice", types.ActionWrite), handlers.Invoice.FinalizeInvoice)
			invoices.POST("/:id/compute", write("invoice", types.ActionWrite), handlers.Invoice.ComputeInvoice)
			invoices.POST("/:id/void", write("invoice", types.ActionWrite), handlers.Invoice.VoidInvoice)
			invoices.POST("/preview", handlers.Invoice.GetPreviewInvoice)
			invoices.POST("/internal/preview", handlers.Invoice.GetInternalPreviewInvoice)
			invoices.POST("/meter-usage-preview", handlers.Invoice.GetMeterUsagePreviewInvoice)
			invoices.PUT("/:id/payment", write("invoice", types.ActionWrite), handlers.Invoice.UpdatePaymentStatus)
			invoices.POST("/:id/payment/attempt", write("invoice", types.ActionWrite), handlers.Invoice.AttemptPayment)
			invoices.GET("/:id/pdf", handlers.Invoice.GetInvoicePDF)
			invoices.POST("/:id/recalculate", write("invoice", types.ActionWrite), handlers.Invoice.RecalculateInvoice)
			invoices.POST("/:id/recalculate-v2", write("invoice", types.ActionWrite), handlers.Invoice.RecalculateInvoiceV2)
			invoices.POST("/:id/comms/trigger", write("invoice", types.ActionWrite), handlers.Invoice.TriggerCommunication)
			invoices.POST("/:id/webhook/trigger", write("invoice", types.ActionWrite), handlers.Invoice.TriggerWebhook)
		}

		feature := v1Private.Group("/features")
		{
			feature.POST("", write("feature", types.ActionWrite), handlers.Feature.CreateFeature)
			feature.GET("", handlers.Feature.ListFeatures)
			feature.GET("/:id", handlers.Feature.GetFeature)
			feature.PUT("/:id", write("feature", types.ActionWrite), handlers.Feature.UpdateFeature)
			feature.DELETE("/:id", write("feature", types.ActionWrite), handlers.Feature.DeleteFeature)
			feature.POST("/search", handlers.Feature.QueryFeatures)
			feature.POST("/:id/clone", write("feature", types.ActionWrite), handlers.Feature.CloneFeature)
		}

		entitlement := v1Private.Group("/entitlements")
		{
			entitlement.POST("/search", handlers.Entitlement.QueryEntitlements)
			entitlement.POST("", write("entitlement", types.ActionWrite), handlers.Entitlement.CreateEntitlement)
			entitlement.POST("/bulk", write("entitlement", types.ActionWrite), handlers.Entitlement.CreateBulkEntitlement)
			entitlement.GET("", handlers.Entitlement.ListEntitlements)
			entitlement.GET("/:id", handlers.Entitlement.GetEntitlement)
			entitlement.PUT("/:id", write("entitlement", types.ActionWrite), handlers.Entitlement.UpdateEntitlement)
			entitlement.DELETE("/:id", write("entitlement", types.ActionWrite), handlers.Entitlement.DeleteEntitlement)
		}

		creditGrant := v1Private.Group("/creditgrants")
		{
			creditGrant.POST("", write("creditgrant", types.ActionWrite), handlers.CreditGrant.CreateCreditGrant)
			creditGrant.GET("", handlers.CreditGrant.ListCreditGrants)
			creditGrant.GET("/:id", handlers.CreditGrant.GetCreditGrant)
			creditGrant.PUT("/:id", write("creditgrant", types.ActionWrite), handlers.CreditGrant.UpdateCreditGrant)
			creditGrant.DELETE("/:id", write("creditgrant", types.ActionWrite), handlers.CreditGrant.DeleteCreditGrant)
		}

		payments := v1Private.Group("/payments")
		{
			payments.POST("", write("payment", types.ActionWrite), handlers.Payment.CreatePayment)
			payments.GET("", handlers.Payment.ListPayments)
			payments.GET("/:id", handlers.Payment.GetPayment)
			payments.PUT("/:id", write("payment", types.ActionWrite), handlers.Payment.UpdatePayment)
			payments.DELETE("/:id", write("payment", types.ActionWrite), handlers.Payment.DeletePayment)
			payments.POST("/:id/process", write("payment", types.ActionWrite), handlers.Payment.ProcessPayment)

			custPaymentsGroup := payments.Group("/customers")
			{
				custPaymentsGroup.GET("/:id/methods", handlers.SetupIntent.ListCustomerPaymentMethods)
				custPaymentsGroup.POST("/:id/setup/intent", write("payment", types.ActionWrite), handlers.SetupIntent.CreateSetupIntentSession)
			}
		}

		tasks := v1Private.Group("/tasks")
		{
			tasks.POST("", write("task", types.ActionWrite), handlers.Task.CreateTask)
			tasks.GET("", handlers.Task.ListTasks)
			tasks.GET("/:id", handlers.Task.GetTask)
			tasks.PUT("/:id/status", write("task", types.ActionWrite), handlers.Task.UpdateTaskStatus)
			tasks.GET("/:id/download", handlers.Task.DownloadTaskFile)

			// Scheduled tasks routes under /tasks/scheduled
			scheduledTasks := tasks.Group("/scheduled")
			{
				scheduledTasks.POST("", write("task", types.ActionWrite), handlers.ScheduledTask.CreateScheduledTask)
				scheduledTasks.GET("", handlers.ScheduledTask.ListScheduledTasks)
				scheduledTasks.GET("/:id", handlers.ScheduledTask.GetScheduledTask)
				scheduledTasks.PUT("/:id", write("task", types.ActionWrite), handlers.ScheduledTask.UpdateScheduledTask)
				scheduledTasks.DELETE("/:id", write("task", types.ActionWrite), handlers.ScheduledTask.DeleteScheduledTask)
				scheduledTasks.POST("/:id/run", write("task", types.ActionWrite), handlers.ScheduledTask.TriggerForceRun)
			}
		}

		// Tax rate routes
		tax := v1Private.Group("/taxes")
		taxRates := tax.Group("/rates")
		{
			taxRates.POST("", write("tax", types.ActionWrite), handlers.Tax.CreateTaxRate)
			taxRates.GET("", handlers.Tax.ListTaxRates)
			taxRates.GET("/:id", handlers.Tax.GetTaxRate)
			taxRates.PUT("/:id", write("tax", types.ActionWrite), handlers.Tax.UpdateTaxRate)
			taxRates.DELETE("/:id", write("tax", types.ActionWrite), handlers.Tax.DeleteTaxRate)
		}

		taxAssociations := tax.Group("/associations")
		{
			taxAssociations.POST("", write("tax", types.ActionWrite), handlers.Tax.CreateTaxAssociation)
			taxAssociations.GET("", handlers.Tax.ListTaxAssociations)
			taxAssociations.GET("/:id", handlers.Tax.GetTaxAssociation)
			taxAssociations.PUT("/:id", write("tax", types.ActionWrite), handlers.Tax.UpdateTaxAssociation)
			taxAssociations.DELETE("/:id", write("tax", types.ActionWrite), handlers.Tax.DeleteTaxAssociation)
		}

		// Secret routes
		secrets := v1Private.Group("/secrets")
		{
			// API Key routes
			apiKeys := secrets.Group("/api/keys")
			{
				apiKeys.GET("", handlers.Secret.ListAPIKeys)
				apiKeys.POST("", write("secret", types.ActionWrite), handlers.Secret.CreateAPIKey)
				apiKeys.DELETE("/:id", write("secret", types.ActionWrite), handlers.Secret.DeleteAPIKey)
			}
		}

		// Connection routes
		connections := v1Private.Group("/connections")
		{
			connections.POST("", write("connection", types.ActionWrite), handlers.Connection.CreateConnection)
			connections.GET("", handlers.Connection.ListConnections)
			connections.GET("/:id", handlers.Connection.GetConnection)
			connections.PUT("/:id", write("connection", types.ActionWrite), handlers.Connection.UpdateConnection)
			connections.DELETE("/:id", write("connection", types.ActionWrite), handlers.Connection.DeleteConnection)
			connections.POST("/search", handlers.Connection.QueryConnections)
		}

		// Costsheet routes
		costsheets := v1Private.Group("/costs")
		{
			costsheets.POST("/search", handlers.Costsheet.QueryCostsheets)
			costsheets.POST("", write("costsheet", types.ActionWrite), handlers.Costsheet.CreateCostsheet)
			costsheets.GET("/:id", handlers.Costsheet.GetCostsheet)
			costsheets.PUT("/:id", write("costsheet", types.ActionWrite), handlers.Costsheet.UpdateCostsheet)
			costsheets.DELETE("/:id", write("costsheet", types.ActionWrite), handlers.Costsheet.DeleteCostsheet)
			costsheets.GET("/active", handlers.Costsheet.GetActiveCostsheetForTenant)
			costsheets.POST("/analytics", handlers.RevenueAnalytics.GetDetailedCostAnalytics)
			costsheets.POST("/analytics-v2", handlers.RevenueAnalytics.GetDetailedCostAnalyticsV2)
		}

		// Credit note routes
		creditNotes := v1Private.Group("/creditnotes")
		{
			creditNotes.POST("", write("creditnote", types.ActionWrite), handlers.CreditNote.CreateCreditNote)
			creditNotes.GET("", handlers.CreditNote.ListCreditNotes)
			creditNotes.GET("/:id", handlers.CreditNote.GetCreditNote)
			creditNotes.POST("/:id/void", write("creditnote", types.ActionWrite), handlers.CreditNote.VoidCreditNote)
			creditNotes.POST("/:id/finalize", write("creditnote", types.ActionWrite), handlers.CreditNote.FinalizeCreditNote)
		}

		// Integration routes
		integrations := v1Private.Group("/integrations")
		{
			integrations.POST("/link", handlers.Integration.Link)
			integrations.POST("/sync", handlers.Integration.Sync)
			integrations.GET("/mappings", handlers.Integration.GetMappings)
			integrations.GET("/config", handlers.Integration.GetConfig)
			// paddleGroup := integrations.Group("/paddle")
			// {
			// 	paddleGroup.POST("/invoices/:invoice_id/sync", handlers.Paddle.SyncInvoice)
			// }
		}

		// Coupon routes
		coupon := v1Private.Group("/coupons")
		{
			coupon.POST("", write("coupon", types.ActionWrite), handlers.Coupon.CreateCoupon)
			coupon.GET("", handlers.Coupon.ListCoupons)
			coupon.GET("/:id", handlers.Coupon.GetCoupon)
			coupon.PUT("/:id", write("coupon", types.ActionWrite), handlers.Coupon.UpdateCoupon)
			coupon.DELETE("/:id", write("coupon", types.ActionWrite), handlers.Coupon.DeleteCoupon)
			coupon.POST("/search", handlers.Coupon.QueryCoupons)
		}

		// Admin routes (API Key only)
		adminRoutes := v1Private.Group("/admin")
		adminRoutes.Use(middleware.APIKeyAuthMiddleware(cfg, secretService, logger))
		{
			// All admin routes to go here
		}

		// AI helpers (authenticated; same middleware as other /v1 private routes)
		aiRoutes := v1Private.Group("/ai")
		{
			aiPricing := aiRoutes.Group("/pricing")
			{
				aiPricing.POST("/parse-gemini", write("ai", types.ActionWrite), handlers.AIPricing.ParseGeminiPricing)
			}
		}

		// Portal routes (UI-specific endpoints)
		portalRoutes := v1Private.Group("/portal")
		{
			onboarding := portalRoutes.Group("/onboarding")
			{
				onboarding.POST("/events", write("portal", types.ActionWrite), handlers.Onboarding.GenerateEvents)
				onboarding.POST("/setup", write("portal", types.ActionWrite), handlers.Onboarding.SetupDemo)
			}
		}

		// Webhook routes
		webhookGroup := v1Private.Group("/webhooks")
		{
			webhookGroup.GET("/dashboard", handlers.Webhook.GetDashboardURL)
			webhookGroup.POST("/retry", write("webhook", types.ActionWrite), handlers.Webhook.RetryOutboundWebhook)
		}
	}

	// Customer Dashboard - Customer-facing APIs (requires dashboard token)
	customerPortalAPI := router.Group("/v1/customer/portal")
	customerPortalAPI.Use(middleware.SessionTokenAuthMiddleware(cfg, logger))
	customerPortalAPI.Use(middleware.ErrorHandler())
	{
		// Customer specific
		customerPortalAPI.GET("/info", handlers.CustomerPortal.GetCustomer)
		customerPortalAPI.PUT("/info", handlers.CustomerPortal.UpdateCustomer)
		customerPortalAPI.GET("/usage", handlers.CustomerPortal.GetUsageSummary)

		// Subscriptions
		customerPortalAPI.POST("/subscriptions", handlers.CustomerPortal.GetSubscriptions)
		customerPortalAPI.GET("/subscriptions/:id", handlers.CustomerPortal.GetSubscription)

		// Invoices
		customerPortalAPI.POST("/invoices", handlers.CustomerPortal.GetInvoices)
		customerPortalAPI.GET("/invoices/:id", handlers.CustomerPortal.GetInvoice)
		customerPortalAPI.GET("/invoices/:id/pdf", handlers.CustomerPortal.GetInvoicePDF)

		// Wallets
		customerPortalAPI.POST("/wallets", handlers.CustomerPortal.GetWallets)
		customerPortalAPI.GET("/wallets/:id", handlers.CustomerPortal.GetWallet)
		customerPortalAPI.GET("/wallets/:id/transactions", handlers.CustomerPortal.GetWalletTransactions)

		// Portal config (theme, sections, tabs)
		customerPortalAPI.GET("/config", handlers.CustomerPortal.GetPortalConfig)

		// Analytics
		customerPortalAPI.POST("/analytics/revenue", handlers.CustomerPortal.GetAnalytics)

		// Cost Analytics
		customerPortalAPI.POST("/analytics/cost", handlers.CustomerPortal.GetCostAnalytics)
	}

	// Public webhook endpoints (no authentication required)
	webhooks := v1Public.Group("/webhooks")
	{
		// Stripe webhook endpoint: POST /v1/webhooks/stripe/{tenant_id}/{environment_id}
		webhooks.POST("/stripe/:tenant_id/:environment_id", handlers.Webhook.HandleStripeWebhook)
		// HubSpot webhook endpoint: POST /v1/webhooks/hubspot/{tenant_id}/{environment_id}
		webhooks.POST("/hubspot/:tenant_id/:environment_id", handlers.Webhook.HandleHubSpotWebhook)
		// Razorpay webhook endpoint: POST /v1/webhooks/razorpay/{tenant_id}/{environment_id}
		webhooks.POST("/razorpay/:tenant_id/:environment_id", handlers.Webhook.HandleRazorpayWebhook)
		// Chargebee webhook endpoint: POST /v1/webhooks/chargebee/{tenant_id}/{environment_id}
		webhooks.POST("/chargebee/:tenant_id/:environment_id", handlers.Webhook.HandleChargebeeWebhook)
		// QuickBooks webhook endpoint: POST /v1/webhooks/quickbooks/{tenant_id}/{environment_id}
		webhooks.POST("/quickbooks/:tenant_id/:environment_id", handlers.Webhook.HandleQuickBooksWebhook)
		// Nomod webhook endpoint: POST /v1/webhooks/nomod/{tenant_id}/{environment_id}
		webhooks.POST("/nomod/:tenant_id/:environment_id", handlers.Webhook.HandleNomodWebhook)
		// Moyasar webhook endpoint: POST /v1/webhooks/moyasar/{tenant_id}/{environment_id}
		webhooks.POST("/moyasar/:tenant_id/:environment_id", handlers.Webhook.HandleMoyasarWebhook)
		// Paddle webhook endpoint: POST /v1/webhooks/paddle/{tenant_id}/{environment_id}
		webhooks.POST("/paddle/:tenant_id/:environment_id", handlers.Webhook.HandlePaddleWebhook)
		// Zoho Books webhook endpoint: POST /v1/webhooks/zoho_books/{tenant_id}/{environment_id}
		webhooks.POST("/zoho_books/:tenant_id/:environment_id", handlers.Webhook.HandleZohoBooksWebhook)
		// Whop webhook endpoint: POST /v1/webhooks/whop/{tenant_id}/{environment_id}
		webhooks.POST("/whop/:tenant_id/:environment_id", handlers.Webhook.HandleWhopWebhook)
	}

	// HTTP cron: optional manual/legacy triggers (deprecated for automation; Temporal workers ensure server schedules on startup).
	cron := v1Private.Group("/cron")
	subscriptionGroup := cron.Group("/subscriptions")
	{
		subscriptionGroup.POST("/update-periods", write("cron", types.ActionWrite), handlers.CronSubscription.UpdateBillingPeriods)
		// Deprecated: automation uses Temporal schedule subscription-trial-end-due.
		subscriptionGroup.POST("/process-trial-end-due", write("cron", types.ActionWrite), handlers.CronSubscription.ProcessTrialEndDue)
		subscriptionGroup.POST("/process-auto-cancellation", write("cron", types.ActionWrite), handlers.CronSubscription.ProcessAutoCancellationSubscriptions)
		subscriptionGroup.POST("/renewal-due-alerts", write("cron", types.ActionWrite), handlers.CronSubscription.ProcessSubscriptionRenewalDueAlerts)
	}
	walletGroup := cron.Group("/wallets")
	{
		walletGroup.POST("/expire-credits", write("cron", types.ActionWrite), handlers.CronWallet.ExpireCredits)
	}
	creditGrantGroup := cron.Group("/creditgrants")
	{
		creditGrantGroup.POST("/process-scheduled-applications", write("cron", types.ActionWrite), handlers.CronCreditGrant.ProcessScheduledCreditGrantApplications)
	}
	invoiceGroup := cron.Group("/invoices")
	{
		invoiceGroup.POST("/void-old-pending", write("cron", types.ActionWrite), handlers.CronInvoice.VoidOldPendingInvoices)
	}
	kafkaLagMonitoringGroup := cron.Group("/events")
	{
		kafkaLagMonitoringGroup.POST("/monitoring", write("cron", types.ActionWrite), handlers.CronKafkaLagMonitoring.HandleKafkaLagMonitoring)
	}

	// Settings routes
	settings := v1Private.Group("/settings")
	{
		settings.GET("/:key", handlers.Settings.GetSettingByKey)
		settings.PUT("/:key", write("setting", types.ActionWrite), handlers.Settings.UpdateSettingByKey)
		settings.DELETE("/:key", write("setting", types.ActionWrite), handlers.Settings.DeleteSettingByKey)
	}

	// Alert routes
	alert := v1Private.Group("/alerts")
	{
		// list alert logs by filter
		alert.POST("/search", handlers.AlertLogsHandler.QueryAlertLogs)
	}

	// RBAC routes
	rbac := v1Private.Group("/rbac")
	{
		rbac.GET("/roles", handlers.RBAC.ListRoles)
		rbac.GET("/roles/:id", handlers.RBAC.GetRole)
	}

	// OAuth routes
	oauth := v1Private.Group("/oauth")
	{
		oauth.POST("/init", write("oauth", types.ActionWrite), handlers.OAuth.InitiateOAuth)
		oauth.POST("/complete", write("oauth", types.ActionWrite), handlers.OAuth.CompleteOAuth)
	}

	// Dashboard routes
	dashboardRoutes := v1Private.Group("/dashboard")
	{
		dashboardRoutes.POST("/revenues", handlers.Dashboard.GetRevenues)
		dashboardRoutes.POST("/revenue-dashboard", handlers.Dashboard.GetRevenueDashboard)
	}

	// Workflow monitoring routes
	workflows := v1Private.Group("/workflows")
	{
		workflows.POST("/search", handlers.Workflow.QueryWorkflows)
		workflows.POST("/batch", handlers.Workflow.GetWorkflowsBatch)
		workflows.GET("/:workflow_id/:run_id/summary", handlers.Workflow.GetWorkflowSummary)
		workflows.GET("/:workflow_id/:run_id/timeline", handlers.Workflow.GetWorkflowTimeline)
		workflows.GET("/:workflow_id/:run_id", handlers.Workflow.GetWorkflowDetails)
	}

	return router
}
