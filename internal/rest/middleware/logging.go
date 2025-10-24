package middleware

import (
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// LoggingMiddleware returns a gin middleware that logs HTTP requests using our standard logger
func LoggingMiddleware(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Start timer
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Process request
		c.Next()

		// Calculate latency
		latency := time.Since(start)

		fields := []interface{}{
			"status", c.Writer.Status(),
			"method", c.Request.Method,
			"path", path,
			"query", raw,
			"latency_ms", latency.Milliseconds(),
		}

		if requestID, ok := c.Request.Context().Value(types.CtxRequestID).(string); ok && requestID != "" {
			fields = append(fields, "request_id", requestID)
		}

		// Add error if any
		if len(c.Errors) > 0 {
			fields = append(fields, "errors", c.Errors.String())
		}

		// Add tenant and user info if available
		if tenantID := c.GetString("tenant_id"); tenantID != "" {
			fields = append(fields, "tenant_id", tenantID)
		}
		if userID := c.GetString("user_id"); userID != "" {
			fields = append(fields, "user_id", userID)
		}
		if environmentID := c.GetString("environment_id"); environmentID != "" {
			fields = append(fields, "environment_id", environmentID)
		}

		// Log based on status code
		statusCode := c.Writer.Status()
		switch {
		case statusCode >= 500:
			log.Errorw("HTTP_REQUEST_ERROR", fields...)
		case statusCode >= 400:
			log.Warnw("HTTP_REQUEST_WARNING", fields...)
		default:
			log.Infow("HTTP_REQUEST_INFO", fields...)
		}
	}
}
