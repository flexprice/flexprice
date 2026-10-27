package middleware

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/grafana/pyroscope-go"
)

// PyroscopeMiddleware tags profiles with method and route template.
func PyroscopeMiddleware(cfg *config.Configuration) gin.HandlerFunc {
	if !cfg.Pyroscope.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		labels := pyroscope.Labels("method", c.Request.Method, "endpoint", c.FullPath())
		pyroscope.TagWrapper(context.Background(), labels, func(ctx context.Context) {
			c.Next()
		})
	}
}
