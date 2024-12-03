package api

import (
	"github.com/flexprice/flexprice/docs/swagger"
	v1 "github.com/flexprice/flexprice/internal/api/v1"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"
)

type Handlers struct {
	Events *v1.EventsHandler
	Meter  *v1.MeterHandler
	Auth   *v1.AuthHandler
	User   *v1.UserHandler
	Health *v1.HealthHandler
}

func NewRouter(handlers Handlers, cfg *config.Configuration, logger *logger.Logger) *gin.Engine {
	router := gin.Default()
	router.Use(
		middleware.RequestIDMiddleware,
		middleware.CORSMiddleware,
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
			events.GET("/usage", handlers.Events.GetUsage)
			events.GET("/usage/meter", handlers.Events.GetUsageByMeter)
		}

		meters := v1Private.Group("/meters")
		{
			meters.POST("", handlers.Meter.CreateMeter)
			meters.GET("", handlers.Meter.GetAllMeters)
			meters.GET("/:id", handlers.Meter.GetMeter)
			meters.POST("/:id/disable", handlers.Meter.DisableMeter)
			meters.DELETE("/:id", handlers.Meter.DeleteMeter)
			meters.POST("/sync/stripe", handlers.Meter.SyncUsageToStripe)
		}
	}
	return router
}
