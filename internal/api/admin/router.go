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
	Health      *v1.HealthHandler
	Environment *v1.EnvironmentHandler
	Tenant      *v1.TenantHandler
	User        *v1.UserHandler
	Settings    *v1.SettingsHandler
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

	engine.GET("/health", handlers.Health.Health)
	engine.POST("/health", handlers.Health.Health)

	// Shared secret from admin.secret. Cross-tenant writes stay intentional for operators.
	group := engine.Group("/v1")
	group.Use(middleware.ErrorHandler())
	group.Use(requireSecret(secret))
	{
		environment := group.Group("/environments")
		{
			environment.POST("", handlers.Environment.CreateEnvironment)
		}

		tenant := group.Group("/tenants")
		{
			tenant.POST("", handlers.Tenant.CreateTenant)
		}

		user := group.Group("/users")
		{
			user.POST("", handlers.User.AddUser)
		}

		settings := group.Group("/settings")
		{
			settings.PUT("/tenant_config", handlers.Settings.UpdateTenantConfig)
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
