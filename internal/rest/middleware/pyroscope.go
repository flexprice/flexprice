package middleware

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/grafana/pyroscope-go"
)

// PyroscopeMiddleware returns a middleware that adds profiling labels to HTTP requests
func PyroscopeMiddleware(cfg *config.Configuration) gin.HandlerFunc {
	if !cfg.Pyroscope.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return func(c *gin.Context) {
		// Create profiling labels for the HTTP request
		labels := map[string]string{
			"method":   c.Request.Method,
			"endpoint": c.FullPath(),
			"handler":  fmt.Sprintf("%s %s", c.Request.Method, c.FullPath()),
		}

		// If there are route parameters, add them as labels
		if len(c.Params) > 0 {
			for _, param := range c.Params {
				labels[fmt.Sprintf("param_%s", param.Key)] = param.Value
			}
		}

		// Convert map to pyroscope labels format
		var labelPairs []string
		for key, value := range labels {
			labelPairs = append(labelPairs, key, value)
		}

		// Wrap the request processing with profiling labels
		pyroscope.TagWrapper(context.Background(), pyroscope.Labels(labelPairs...), func(ctx context.Context) {
			c.Next()
		})
	}
}
