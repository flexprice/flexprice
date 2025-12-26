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
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Handlers struct {
	Events                   *v1.EventsHandler
	Meter                    *v1.MeterHandler
	Auth                     *v1.AuthHandler
	User                     *v1.UserHandler
	Environment              *v1.EnvironmentHandler
	Health                   *v1.HealthHandler
	Price                    *v1.PriceHandler
	Customer                 *v1.CustomerHandler
	Connection               *v1.ConnectionHandler
	Plan                     *v1.PlanHandler
	Subscription             *v1.SubscriptionHandler
	SubscriptionPause        *v1.SubscriptionPauseHandler
	SubscriptionChange       *v1.SubscriptionChangeHandler
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
	PriceUnit                *v1.PriceUnitHandler
	Webhook                  *v1.WebhookHandler
	Addon                    *v1.AddonHandler
	EntityIntegrationMapping *v1.EntityIntegrationMappingHandler
	Settings                 *v1.SettingsHandler
	SetupIntent              *v1.SetupIntentHandler
	Group                    *v1.GroupHandler
	ScheduledTask            *v1.ScheduledTaskHandler
	AlertLogsHandler         *v1.AlertLogsHandler
	RBAC                     *v1.RBACHandler
	OAuth                    *v1.OAuthHandler

	// Portal handlers
	Onboarding *v1.OnboardingHandler
	// Cron jobs : TODO: move crons out of API based architecture
	CronSubscription       *cron.SubscriptionHandler
	CronWallet             *cron.WalletCronHandler
	CronCreditGrant        *cron.CreditGrantCronHandler
	CronInvoice            *cron.InvoiceHandler
	CronKafkaLagMonitoring *cron.KafkaLagMonitoringHandler
}

func NewRouter(handlers Handlers, cfg *config.Configuration, logger *logger.Logger, secretService service.SecretService, envAccessService service.EnvAccessService, rbacService *rbac.RBACService) *gin.Engine {
	// gin.SetMode(gin.ReleaseMode)

	router := gin.Default()
	router.Use(
		middleware.RequestIDMiddleware,
		middleware.CORSMiddleware,
		middleware.SentryMiddleware(cfg),    // Add Sentry middleware
		middleware.PyroscopeMiddleware(cfg), // Add Pyroscope middleware
	)

	// Initialize permission middleware
	permissionMW := middleware.NewPermissionMiddleware(rbacService, logger)

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
	// Swagger documentation
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

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
	private.Use(middleware.EnvAccessMiddleware(envAccessService, logger))

	v1Private := private.Group("/v1")
	v1Private.Use(middleware.ErrorHandler())
	{
		user := v1Private.Group("/users")
		{
			user.GET("/me", handlers.User.GetUserInfo)
			user.POST("", handlers.User.CreateUser)
			user.POST("/search", handlers.User.ListUsersByFilter)
		}

		environment := v1Private.Group("/environments")
		{
			environment.POST("", handlers.Environment.CreateEnvironment)
			environment.GET("", handlers.Environment.GetEnvironments)
			environment.GET("/:id", handlers.Environment.GetEnvironment)
			environment.PUT("/:id", handlers.Environment.UpdateEnvironment)
		}

		// Events routes
		events := v1Private.Group("/events")
		{
			events.POST("", permissionMW.RequirePermission("event", "write"), handlers.Events.IngestEvent)
			events.POST("/bulk", permissionMW.RequirePermission("event", "write"), handlers.Events.BulkIngestEvent)
			events.GET("", handlers.Events.GetEvents)
			events.POST("/query", handlers.Events.QueryEvents)
			events.POST("/usage", handlers.Events.GetUsage)
			events.POST("/usage/meter", handlers.Events.GetUsageByMeter)
			events.POST("/analytics", handlers.Events.GetUsageAnalytics)
			events.POST("/analytics-v2", handlers.Events.GetUsageAnalyticsV2)
			events.POST("/huggingface-billing", handlers.Events.GetHuggingFaceBillingData)
			events.GET("/monitoring", handlers.Events.GetMonitoringData)
		}

		meters := v1Private.Group("/meters")
		{
			meters.POST("", handlers.Meter.CreateMeter)
			meters.GET("", handlers.Meter.GetAllMeters)
			meters.GET("/:id", handlers.Meter.GetMeter)
			meters.POST("/:id/disable", handlers.Meter.DisableMeter)
			meters.DELETE("/:id", handlers.Meter.DeleteMeter)
			meters.PUT("/:id", handlers.Meter.UpdateMeter)
		}

		price := v1Private.Group("/prices")
		{
			price.POST("", handlers.Price.CreatePrice)
			price.POST("/bulk", handlers.Price.CreateBulkPrice)
			price.GET("", handlers.Price.GetPrices)
			price.GET("/:id", handlers.Price.GetPrice)
			price.PUT("/:id", handlers.Price.UpdatePrice)
			price.DELETE("/:id", handlers.Price.DeletePrice)

			priceUnit := price.Group("/units")
			{
				priceUnit.POST("", handlers.PriceUnit.CreatePriceUnit)
				priceUnit.GET("", handlers.PriceUnit.GetPriceUnits)
				priceUnit.GET("/:id", handlers.PriceUnit.GetByID)
				priceUnit.GET("/code/:code", handlers.PriceUnit.GetByCode)
				priceUnit.PUT("/:id", handlers.PriceUnit.UpdatePriceUnit)
				priceUnit.DELETE("/:id", handlers.PriceUnit.DeletePriceUnit)
				priceUnit.POST("/search", handlers.PriceUnit.ListPriceUnitsByFilter)
			}
		}

		customer := v1Private.Group("/customers")
		{

			// list customers by filter
			customer.POST("/search", handlers.Customer.ListCustomersByFilter)

			customer.POST("", handlers.Customer.CreateCustomer)
			customer.GET("", handlers.Customer.GetCustomers)
			customer.GET("/:id", handlers.Customer.GetCustomer)
			customer.PUT("/:id", handlers.Customer.UpdateCustomer)
			customer.DELETE("/:id", handlers.Customer.DeleteCustomer)
			customer.GET("/lookup/:lookup_key", handlers.Customer.GetCustomerByLookupKey)

			// New endpoints for entitlements and usage
			customer.GET("/:id/entitlements", handlers.Customer.GetCustomerEntitlements)
			customer.GET("/usage", handlers.Customer.GetCustomerUsageSummary)     // New route with query parameters (must come first!)
			customer.GET("/:id/usage", handlers.Customer.GetCustomerUsageSummary) // Deprecated route with path parameter
			customer.GET("/:id/grants/upcoming", handlers.Customer.GetUpcomingCreditGrantApplications)

			// other routes for customer
			customer.GET("/:id/wallets", handlers.Wallet.GetWalletsByCustomerID)
			customer.GET("/:id/invoices/summary", handlers.Invoice.GetCustomerInvoiceSummary)
			customer.GET("/wallets", handlers.Wallet.GetCustomerWallets)

		}

		plan := v1Private.Group("/plans")
		{
			// list plans by filter
			plan.POST("/search", handlers.Plan.ListPlansByFilter)

			plan.POST("", handlers.Plan.CreatePlan)
			plan.GET("", handlers.Plan.GetPlans)
			plan.GET("/:id", handlers.Plan.GetPlan)
			plan.PUT("/:id", handlers.Plan.UpdatePlan)
			plan.DELETE("/:id", handlers.Plan.DeletePlan)
			plan.POST("/:id/sync/subscriptions", handlers.Plan.SyncPlanPrices)

			// entitlement routes
			plan.GET("/:id/entitlements", handlers.Plan.GetPlanEntitlements)
			plan.GET("/:id/creditgrants", handlers.Plan.GetPlanCreditGrants)
		}

		addon := v1Private.Group("/addons")
		{
			// list addons by filter
			addon.POST("/search", handlers.Addon.ListAddonsByFilter)

			addon.POST("", handlers.Addon.CreateAddon)
			addon.GET("", handlers.Addon.GetAddons)
			addon.GET("/:id", handlers.Addon.GetAddon)
			addon.GET("/lookup/:lookup_key", handlers.Addon.GetAddonByLookupKey)
			addon.PUT("/:id", handlers.Addon.UpdateAddon)
			addon.GET("/:id/entitlements", handlers.Addon.GetAddonEntitlements)
			addon.DELETE("/:id", handlers.Addon.DeleteAddon)
		}

		group := v1Private.Group("/groups")
		{
			group.POST("", handlers.Group.CreateGroup)
			group.POST("/search", handlers.Group.ListGroups)
			group.GET("/:id", handlers.Group.GetGroup)
			group.DELETE("/:id", handlers.Group.DeleteGroup)
		}

		subscription := v1Private.Group("/subscriptions")
		{
			subscription.POST("/search", handlers.Subscription.ListSubscriptionsByFilter)
			subscription.POST("", handlers.Subscription.CreateSubscription)
			subscription.GET("", handlers.Subscription.GetSubscriptions)
			subscription.GET("/:id", handlers.Subscription.GetSubscription)
			subscription.POST("/:id/activate", handlers.Subscription.ActivateDraftSubscription)
			subscription.POST("/:id/cancel", handlers.Subscription.CancelSubscription)
			subscription.POST("/usage", handlers.Subscription.GetUsageBySubscription)

			subscription.POST("/:id/pause", handlers.SubscriptionPause.PauseSubscription)
			subscription.POST("/:id/resume", handlers.SubscriptionPause.ResumeSubscription)
			subscription.GET("/:id/pauses", handlers.SubscriptionPause.ListPauses)
			subscription.GET("/:id/entitlements", handlers.Subscription.GetSubscriptionEntitlements)
			subscription.GET("/:id/grants/upcoming", handlers.Subscription.GetUpcomingCreditGrantApplications)

			// Addon management for subscriptions - moved under subscription handler
			subscription.POST("/addon", handlers.Subscription.AddAddonToSubscription)
			subscription.DELETE("/addon", handlers.Subscription.RemoveAddonToSubscription)
			subscription.GET("/:id/addons/associations", handlers.Subscription.GetActiveAddonAssociations)

			// Subscription plan changes (upgrade/downgrade)
			subscription.POST("/:id/change/preview", handlers.SubscriptionChange.PreviewSubscriptionChange)
			subscription.POST("/:id/change/execute", handlers.SubscriptionChange.ExecuteSubscriptionChange)

			// Subscription line item management
			subscription.PUT("/lineitems/:id", handlers.Subscription.UpdateSubscriptionLineItem)
			subscription.DELETE("/lineitems/:id", handlers.Subscription.DeleteSubscriptionLineItem)

		}

		wallet := v1Private.Group("/wallets")
		{
			wallet.POST("", handlers.Wallet.CreateWallet)
			wallet.GET("", handlers.Wallet.ListWallets)
			wallet.GET("/:id", handlers.Wallet.GetWalletByID)
			wallet.GET("/:id/transactions", handlers.Wallet.GetWalletTransactions)
			wallet.POST("/:id/top-up", handlers.Wallet.TopUpWallet)
			wallet.POST("/:id/terminate", handlers.Wallet.TerminateWallet)
			wallet.GET("/:id/balance/real-time", handlers.Wallet.GetWalletBalance)
			wallet.GET("/:id/balance/real-time-v2", handlers.Wallet.GetWalletBalanceV2)
			wallet.PUT("/:id", handlers.Wallet.UpdateWallet)
			wallet.POST("/:id/debit", handlers.Wallet.ManualBalanceDebit)
			wallet.POST("/transactions/search", handlers.Wallet.ListWalletTransactionsByFilter)
			wallet.POST("/search", handlers.Wallet.ListWalletsByFilter)
		}
		// Tenant routes
		tenantRoutes := v1Private.Group("/tenants")
		{
			tenantRoutes.POST("", handlers.Tenant.CreateTenant)
			tenantRoutes.PUT("/update", handlers.Tenant.UpdateTenant)
			tenantRoutes.GET("/:id", handlers.Tenant.GetTenantByID)
			tenantRoutes.GET("/billing", handlers.Tenant.GetTenantBillingUsage)
		}

		invoices := v1Private.Group("/invoices")
		{
			invoices.POST("/search", handlers.Invoice.ListInvoicesByFilter)
			invoices.POST("", handlers.Invoice.CreateOneOffInvoice)
			invoices.GET("", handlers.Invoice.ListInvoices)
			invoices.GET("/:id", handlers.Invoice.GetInvoice)
			invoices.PUT("/:id", handlers.Invoice.UpdateInvoice)
			invoices.POST("/:id/finalize", handlers.Invoice.FinalizeInvoice)
			invoices.POST("/:id/void", handlers.Invoice.VoidInvoice)
			invoices.POST("/preview", handlers.Invoice.GetPreviewInvoice)
			invoices.PUT("/:id/payment", handlers.Invoice.UpdatePaymentStatus)
			invoices.POST("/:id/payment/attempt", handlers.Invoice.AttemptPayment)
			invoices.GET("/:id/pdf", handlers.Invoice.GetInvoicePDF)
			invoices.POST("/:id/recalculate", handlers.Invoice.RecalculateInvoice)
			invoices.POST("/:id/comms/trigger", handlers.Invoice.TriggerCommunication)
		}

		feature := v1Private.Group("/features")
		{

			feature.POST("", handlers.Feature.CreateFeature)
			feature.GET("", handlers.Feature.ListFeatures)
			feature.GET("/:id", handlers.Feature.GetFeature)
			feature.PUT("/:id", handlers.Feature.UpdateFeature)
			feature.DELETE("/:id", handlers.Feature.DeleteFeature)
			feature.POST("/search", handlers.Feature.ListFeaturesByFilter)
		}

		entitlement := v1Private.Group("/entitlements")
		{
			entitlement.POST("/search", handlers.Entitlement.ListEntitlementsByFilter)
			entitlement.POST("", handlers.Entitlement.CreateEntitlement)
			entitlement.POST("/bulk", handlers.Entitlement.CreateBulkEntitlement)
			entitlement.GET("", handlers.Entitlement.ListEntitlements)
			entitlement.GET("/:id", handlers.Entitlement.GetEntitlement)
			entitlement.PUT("/:id", handlers.Entitlement.UpdateEntitlement)
			entitlement.DELETE("/:id", handlers.Entitlement.DeleteEntitlement)
		}

		creditGrant := v1Private.Group("/creditgrants")
		{
			creditGrant.POST("", handlers.CreditGrant.CreateCreditGrant)
			creditGrant.GET("", handlers.CreditGrant.ListCreditGrants)
			creditGrant.GET("/:id", handlers.CreditGrant.GetCreditGrant)
			creditGrant.PUT("/:id", handlers.CreditGrant.UpdateCreditGrant)
			creditGrant.DELETE("/:id", handlers.CreditGrant.DeleteCreditGrant)
		}

		payments := v1Private.Group("/payments")
		{
			payments.POST("", handlers.Payment.CreatePayment)
			payments.GET("", handlers.Payment.ListPayments)
			payments.GET("/:id", handlers.Payment.GetPayment)
			payments.PUT("/:id", handlers.Payment.UpdatePayment)
			payments.DELETE("/:id", handlers.Payment.DeletePayment)
			payments.POST("/:id/process", handlers.Payment.ProcessPayment)

			custPaymentsGroup := payments.Group("/customers")
			{
				custPaymentsGroup.GET("/:id/methods", handlers.SetupIntent.ListCustomerPaymentMethods)
				custPaymentsGroup.POST("/:id/setup/intent", handlers.SetupIntent.CreateSetupIntentSession)
			}
		}

		tasks := v1Private.Group("/tasks")
		{
			tasks.POST("", handlers.Task.CreateTask)
			tasks.GET("", handlers.Task.ListTasks)
			tasks.GET("/:id", handlers.Task.GetTask)
			tasks.PUT("/:id/status", handlers.Task.UpdateTaskStatus)

			// Scheduled tasks routes under /tasks/scheduled
			scheduledTasks := tasks.Group("/scheduled")
			{
				scheduledTasks.POST("", handlers.ScheduledTask.CreateScheduledTask)
				scheduledTasks.GET("", handlers.ScheduledTask.ListScheduledTasks)
				scheduledTasks.GET("/:id", handlers.ScheduledTask.GetScheduledTask)
				scheduledTasks.PUT("/:id", handlers.ScheduledTask.UpdateScheduledTask)
				scheduledTasks.DELETE("/:id", handlers.ScheduledTask.DeleteScheduledTask)
				scheduledTasks.POST("/:id/run", handlers.ScheduledTask.TriggerForceRun)

			}
		}

		// Tax rate routes
		tax := v1Private.Group("/taxes")
		taxRates := tax.Group("/rates")
		{
			taxRates.POST("", handlers.Tax.CreateTaxRate)
			taxRates.GET("", handlers.Tax.ListTaxRates)
			taxRates.GET("/:id", handlers.Tax.GetTaxRate)
			taxRates.PUT("/:id", handlers.Tax.UpdateTaxRate)
			taxRates.DELETE("/:id", handlers.Tax.DeleteTaxRate)
		}

		taxAssociations := tax.Group("/associations")
		{
			taxAssociations.POST("", handlers.Tax.CreateTaxAssociation)
			taxAssociations.GET("", handlers.Tax.ListTaxAssociations)
			taxAssociations.GET("/:id", handlers.Tax.GetTaxAssociation)
			taxAssociations.PUT("/:id", handlers.Tax.UpdateTaxAssociation)
			taxAssociations.DELETE("/:id", handlers.Tax.DeleteTaxAssociation)
		}

		// Secret routes
		secrets := v1Private.Group("/secrets")
		{
			// API Key routes
			apiKeys := secrets.Group("/api/keys")
			{
				apiKeys.GET("", handlers.Secret.ListAPIKeys)
				apiKeys.POST("", handlers.Secret.CreateAPIKey)
				apiKeys.DELETE("/:id", handlers.Secret.DeleteAPIKey)
			}

			// Integration routes
			integrations := secrets.Group("/integrations")
			{
				integrations.GET("/linked", handlers.Secret.ListLinkedIntegrations)
				integrations.POST("/:provider", handlers.Secret.CreateIntegration)
				integrations.GET("/:provider", handlers.Secret.GetIntegration)
				integrations.DELETE("/:id", handlers.Secret.DeleteIntegration)
			}
		}

		// Connection routes
		connections := v1Private.Group("/connections")
		{
			connections.POST("", handlers.Connection.CreateConnection)
			connections.GET("", handlers.Connection.GetConnections)
			connections.GET("/:id", handlers.Connection.GetConnection)
			connections.PUT("/:id", handlers.Connection.UpdateConnection)
			connections.DELETE("/:id", handlers.Connection.DeleteConnection)
			connections.POST("/search", handlers.Connection.ListConnectionsByFilter)
		}

		// Costsheet routes
		costsheets := v1Private.Group("/costs")
		{
			costsheets.POST("/search", handlers.Costsheet.ListCostsheetByFilter)
			costsheets.POST("", handlers.Costsheet.CreateCostsheet)
			costsheets.GET("/:id", handlers.Costsheet.GetCostsheet)
			costsheets.PUT("/:id", handlers.Costsheet.UpdateCostsheet)
			costsheets.DELETE("/:id", handlers.Costsheet.DeleteCostsheet)
			costsheets.GET("/active", handlers.Costsheet.GetActiveCostsheetForTenant)
			costsheets.POST("/analytics", handlers.RevenueAnalytics.GetDetailedCostAnalytics)
			costsheets.POST("/analytics-v2", handlers.RevenueAnalytics.GetDetailedCostAnalyticsV2)
		}

		// Credit note routes
		creditNotes := v1Private.Group("/creditnotes")
		{
			creditNotes.POST("", handlers.CreditNote.CreateCreditNote)
			creditNotes.GET("", handlers.CreditNote.ListCreditNotes)
			creditNotes.GET("/:id", handlers.CreditNote.GetCreditNote)
			creditNotes.POST("/:id/void", handlers.CreditNote.VoidCreditNote)
			creditNotes.POST("/:id/finalize", handlers.CreditNote.FinalizeCreditNote)
		}

		// Entity Integration Mapping routes
		entityIntegrationMappings := v1Private.Group("/entity-integration-mappings")
		{
			entityIntegrationMappings.POST("", handlers.EntityIntegrationMapping.CreateEntityIntegrationMapping)
			entityIntegrationMappings.GET("", handlers.EntityIntegrationMapping.ListEntityIntegrationMappings)
			entityIntegrationMappings.GET("/:id", handlers.EntityIntegrationMapping.GetEntityIntegrationMapping)
			entityIntegrationMappings.DELETE("/:id", handlers.EntityIntegrationMapping.DeleteEntityIntegrationMapping)
		}

		// Coupon routes
		coupon := v1Private.Group("/coupons")
		{
			coupon.POST("", handlers.Coupon.CreateCoupon)
			coupon.GET("", handlers.Coupon.ListCouponsByFilter)
			coupon.GET("/:id", handlers.Coupon.GetCoupon)
			coupon.PUT("/:id", handlers.Coupon.UpdateCoupon)
			coupon.DELETE("/:id", handlers.Coupon.DeleteCoupon)
			coupon.POST("/search", handlers.Coupon.ListCouponsByFilter)
		}

		// Admin routes (API Key only)
		adminRoutes := v1Private.Group("/admin")
		adminRoutes.Use(middleware.APIKeyAuthMiddleware(cfg, secretService, logger))
		{
			// All admin routes to go here
		}

		// Portal routes (UI-specific endpoints)
		portalRoutes := v1Private.Group("/portal")
		{
			onboarding := portalRoutes.Group("/onboarding")
			{
				onboarding.POST("/events", handlers.Onboarding.GenerateEvents)
				onboarding.POST("/setup", handlers.Onboarding.SetupDemo)
			}
		}

		// Webhook routes
		webhookGroup := v1Private.Group("/webhooks")
		{
			webhookGroup.GET("/dashboard", handlers.Webhook.GetDashboardURL)
		}
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
	}

	// Cron routes
	// TODO: move crons out of API based architecture
	cron := v1Private.Group("/cron")
	// Subscription related cron jobs
	subscriptionGroup := cron.Group("/subscriptions")
	{
		subscriptionGroup.POST("/update-periods", handlers.CronSubscription.UpdateBillingPeriods)
		subscriptionGroup.POST("/process-auto-cancellation", handlers.CronSubscription.ProcessAutoCancellationSubscriptions)
		subscriptionGroup.POST("/renewal-due-alerts", handlers.CronSubscription.ProcessSubscriptionRenewalDueAlerts)
	}

	// Wallet related cron jobs
	walletGroup := cron.Group("/wallets")
	{
		walletGroup.POST("/expire-credits", handlers.CronWallet.ExpireCredits)
		walletGroup.POST("/check-alerts", handlers.CronWallet.CheckAlerts)
	}

	// Credit grant related cron jobs
	creditGrantGroup := cron.Group("/creditgrants")
	{
		creditGrantGroup.POST("/process-scheduled-applications", handlers.CronCreditGrant.ProcessScheduledCreditGrantApplications)
	}

	// Invoice related cron jobs
	invoiceGroup := cron.Group("/invoices")
	{
		invoiceGroup.POST("/void-old-pending", handlers.CronInvoice.VoidOldPendingInvoices)
	}
	// Kafka lag monitoring related cron jobs
	kafkaLagMonitoringGroup := cron.Group("/events")
	{
		kafkaLagMonitoringGroup.POST("/monitoring", handlers.CronKafkaLagMonitoring.HandleKafkaLagMonitoring)
	}

	// Settings routes
	settings := v1Private.Group("/settings")
	{
		settings.GET("/:key", handlers.Settings.GetSettingByKey)
		settings.PUT("/:key", handlers.Settings.UpdateSettingByKey)
		settings.DELETE("/:key", handlers.Settings.DeleteSettingByKey)
	}

	// Alert routes
	alert := v1Private.Group("/alerts")
	{
		// list alert logs by filter
		alert.POST("/search", handlers.AlertLogsHandler.ListAlertLogsByFilter)
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
		oauth.POST("/init", handlers.OAuth.InitiateOAuth)
		oauth.POST("/complete", handlers.OAuth.CompleteOAuth)
	}

	return router
}
