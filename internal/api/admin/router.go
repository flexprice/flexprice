package admin

import (
	"fmt"
	"net/http"

	v1 "github.com/flexprice/flexprice/internal/api/admin/v1"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
)

// Handlers is the admin portal HTTP surface. Add a handler here, construct it
// in provideAdminHandlers, and register its routes in NewRouter.
type Handlers struct {
	Health             *v1.HealthHandler
	Me                 *v1.MeHandler
	Environment        *v1.EnvironmentHandler
	Tenant             *v1.TenantHandler
	User               *v1.UserHandler
	Settings           *v1.SettingsHandler
	DLQ                *v1.DLQHandler
	RevenueRecognition *v1.RevenueRecognitionHandler
}

// Server is the Gin engine for deployment mode "admin".
// It does not mount the public API.
type Server struct {
	engine *gin.Engine
}

func NewRouter(handlers Handlers, log *logger.Logger, secret string) *Server {
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(middleware.RequestIDMiddleware)
	if log != nil {
		engine.Use(middleware.LoggingMiddleware(log))
	}
	// The dashboard is a different tailnet host from this API, so the browser preflight must succeed.
	engine.Use(middleware.CORSMiddleware)

	engine.GET("/health", handlers.Health.Health)
	engine.POST("/health", handlers.Health.Health)

	// Shared secret from admin.secret. Cross-tenant writes stay intentional for operators.
	group := engine.Group("/v1")
	group.Use(middleware.ErrorHandler())
	group.Use(requireSecret(secret))
	{
		group.GET("/me", handlers.Me.Me)
		group.POST("/dlq/replay", handlers.DLQ.Replay)
		group.GET("/dlq/replay/runs/:workflow_id", handlers.DLQ.GetReplay)

		environment := group.Group("/environments")
		{
			environment.POST("", handlers.Environment.CreateEnvironment)
		}

		tenant := group.Group("/tenants")
		{
			tenant.POST("", handlers.Tenant.CreateTenant)
			tenant.GET("", handlers.Tenant.ListTenants)

			tenantByID := tenant.Group("/:tenant_id")
			{
				tenantByID.GET("", handlers.Tenant.GetTenant)
				tenantByID.GET("/environments", handlers.Environment.ListEnvironments)
				tenantByID.GET("/users", handlers.User.ListUsersForTenant)
				tenantByID.GET("/settings", handlers.Settings.ListTenantSettings)
			}
		}

		user := group.Group("/users")
		{
			user.POST("", handlers.User.AddUser)
			user.POST("/remove", handlers.User.RemoveUser)
		}

		settings := group.Group("/settings")
		{
			settings.PUT("/tenant_config", handlers.Settings.UpdateTenantConfig)
		}

		revenueRecognition := group.Group("/revenue_recognition")
		{
			revenueRecognition.PUT("/config", handlers.RevenueRecognition.UpdateConfig)
			revenueRecognition.POST("/backfill", handlers.RevenueRecognition.StartBackfill)
			revenueRecognition.GET("/backfill/runs/:workflow_id", handlers.RevenueRecognition.GetBackfill)
		}
	}

	return &Server{engine: engine}
}

func (s *Server) Run(addr ...string) error {
	if s == nil || s.engine == nil {
		return fmt.Errorf("admin router is not configured")
	}
	return s.engine.Run(addr...)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if s == nil || s.engine == nil {
		http.Error(w, "admin router is not configured", http.StatusInternalServerError)
		return
	}
	s.engine.ServeHTTP(w, req)
}
