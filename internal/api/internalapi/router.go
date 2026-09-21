package internalapi

import (
	"fmt"

	v1 "github.com/flexprice/flexprice/internal/api/internalapi/v1"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rest/middleware"
	"github.com/gin-gonic/gin"
)

// Handlers is the internal HTTP surface. Add a handler here, construct it in
// provideInternalHandlers, and register its routes in NewRouter.
type Handlers struct {
	Health      *v1.HealthHandler
	Environment *v1.EnvironmentHandler
}

// Server is the Gin engine for deployment mode "internal".
// It does not mount the public API.
type Server struct {
	engine *gin.Engine
}

func NewRouter(handlers Handlers, log *logger.Logger) *Server {
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(middleware.RequestIDMiddleware)
	if log != nil {
		engine.Use(middleware.LoggingMiddleware(log))
	}

	engine.GET("/health", handlers.Health.Health)
	engine.POST("/health", handlers.Health.Health)

	group := engine.Group("/v1")
	group.Use(middleware.ErrorHandler())
	{
		environment := group.Group("/environments")
		{
			environment.POST("", handlers.Environment.CreateEnvironment)
		}
	}

	return &Server{engine: engine}
}

func (s *Server) Run(addr ...string) error {
	if s == nil || s.engine == nil {
		return fmt.Errorf("internal router is not configured")
	}
	return s.engine.Run(addr...)
}
