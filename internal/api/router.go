package api

import (
	"github.com/flexprice/flexprice/docs/swagger"
	"github.com/flexprice/flexprice/internal/api/cron"
	v1 "github.com/flexprice/flexprice/internal/api/v1"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Handlers struct {
	Events       *v1.EventsHandler
	Meter        *v1.MeterHandler
	Auth         *v1.AuthHandler
	User         *v1.UserHandler
	Health       *v1.HealthHandler
	Price        *v1.PriceHandler
	Customer     *v1.CustomerHandler
	Plan         *v1.PlanHandler
	Subscription *v1.SubscriptionHandler
	Wallet       *v1.WalletHandler
	Tenant       *v1.TenantHandler
	Cron         *cron.SubscriptionHandler
}

func NewRouter(handlers Handlers, cfg *config.Configuration, logger *logger.Logger) *gin.Engine {
	// gin.SetMode(gin.ReleaseMode)

	router := gin.Default()
	router.Use(
		middleware.RequestIDMiddleware,
		middleware.CORSMiddleware,
		middleware.SentryMiddleware(cfg), // Add Sentry middleware
	)

	// Add middleware to set swagger host dynamically
	router.Use(func(c *gin.Context) {
		if swagger.SwaggerInfo != nil {
			swagger.SwaggerInfo.Host = c.Request.Host
		}
		c.Next()
	})

	// Health check
	router.GET("/health", handlers.Health.Health)
	// Swagger documentation
	router.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	// Public routes
	public := router.Group("/", middleware.GuestAuthenticateMiddleware)

	v1Public := public.Group("/v1")

	{
		// Auth routes
		v1Public.POST("/auth/signup", handlers.Auth.SignUp)
		v1Public.POST("/auth/login", handlers.Auth.Login)
		v1Public.POST("/events/ingest", handlers.Events.IngestEvent)
	}

	private := router.Group("/", middleware.AuthenticateMiddleware(cfg, logger))

	v1Private := private.Group("/v1")
	{
		user := v1Private.Group("/users")
		{
			user.GET("/me", handlers.User.GetUserInfo)
		}

		// Events routes
		events := v1Private.Group("/events")
		{
			events.POST("", handlers.Events.IngestEvent)
			events.GET("", handlers.Events.GetEvents)
			events.POST("/usage", handlers.Events.GetUsage)
			events.POST("/usage/meter", handlers.Events.GetUsageByMeter)
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
			price.GET("", handlers.Price.GetPrices)
			price.GET("/:id", handlers.Price.GetPrice)
			price.PUT("/:id", handlers.Price.UpdatePrice)
			price.DELETE("/:id", handlers.Price.DeletePrice)
		}

		customer := v1Private.Group("/customers")
		{
			customer.POST("", handlers.Customer.CreateCustomer)
			customer.GET("", handlers.Customer.GetCustomers)
			customer.GET("/:id", handlers.Customer.GetCustomer)
			customer.PUT("/:id", handlers.Customer.UpdateCustomer)
			customer.DELETE("/:id", handlers.Customer.DeleteCustomer)

			// other routes for customer
			customer.GET("/:id/wallets", handlers.Wallet.GetWalletsByCustomerID)
		}

		plan := v1Private.Group("/plans")
		{
			plan.POST("", handlers.Plan.CreatePlan)
			plan.GET("", handlers.Plan.GetPlans)
			plan.GET("/:id", handlers.Plan.GetPlan)
			plan.PUT("/:id", handlers.Plan.UpdatePlan)
			plan.DELETE("/:id", handlers.Plan.DeletePlan)
		}

		subscription := v1Private.Group("/subscriptions")
		{
			subscription.POST("", handlers.Subscription.CreateSubscription)
			subscription.GET("", handlers.Subscription.GetSubscriptions)
			subscription.GET("/:id", handlers.Subscription.GetSubscription)
			subscription.POST("/:id/cancel", handlers.Subscription.CancelSubscription)
			subscription.POST("/usage", handlers.Subscription.GetUsageBySubscription)
		}

		wallet := v1Private.Group("/wallets")
		{
			wallet.POST("", handlers.Wallet.CreateWallet)
			wallet.GET("/:id", handlers.Wallet.GetWalletByID)
			wallet.GET("/:id/transactions", handlers.Wallet.GetWalletTransactions)
			wallet.POST("/:id/top-up", handlers.Wallet.TopUpWallet)
			wallet.POST("/:id/terminate", handlers.Wallet.TerminateWallet)
			wallet.GET("/:id/balance/real-time", handlers.Wallet.GetWalletBalance)
		}
		// Tenant routes
		tenantRoutes := v1Private.Group("/tenants")
		{
			tenantRoutes.POST("", handlers.Tenant.CreateTenant)
			tenantRoutes.GET("/:id", handlers.Tenant.GetTenantByID)
		}

		// Admin routes (API Key only)
		adminRoutes := v1Private.Group("/admin")
		adminRoutes.Use(middleware.APIKeyAuthMiddleware(cfg, logger))
		{
			// All admin routes to go here
		}
	}

	// Cron routes
	// TODO: move crons out of API based architecture
	cron := v1Private.Group("/cron")
	// Subscription related cron jobs
	subscriptionGroup := cron.Group("/subscriptions")
	{
		subscriptionGroup.POST("/update-periods", handlers.Cron.UpdateBillingPeriods)
	}
	return router
}
