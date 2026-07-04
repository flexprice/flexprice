package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/gin-gonic/gin"
)

// LoggingMiddleware returns a gin middleware that logs HTTP requests using our
// standard logger. It binds the request context via WithContext so every log
// line carries trace_id and span_id — enabling native trace-log correlation in
// SigNoz. The binding happens after c.Next() so the otelgin span (created by
// TracingMiddleware, which is registered after LoggingMiddleware) is already
// in the context when the post-request logging fires.
func LoggingMiddleware(log *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		// Run the full handler + downstream middleware chain.
		// otelgin (registered after this middleware) creates the OTel span during
		// c.Next() and attaches it to c.Request.Context(). By the time we return
		// here, the span exists in context and WithContext can extract TraceId/SpanId.
		c.Next()

		// Skip noisy health-check polling from load balancers.
		if path == "/health" {
			return
		}

		latency := time.Since(start)
		statusCode := c.Writer.Status()

		fields := []interface{}{
			"method", c.Request.Method,
			"path", path,
			"query", raw,
			"status", statusCode,
			"latency_ms", latency.Milliseconds(),
		}

		// otelgin's deferred context-restore fires when it returns, which happens
		// BEFORE we reach this post-phase. So c.Request.Context() no longer holds
		// the OTel span. SpanEnrichmentMiddleware (post-phase executes before otelgin's
		// defer) captured both the string IDs and the span-carrying context for us.
		// Use the saved span context (still has span) so:
		//  1. WithContext injects trace_id/span_id as string fields.
		//  2. otelCtxCore injects ctx into otelzap so the OTLP log record's
		//     record-level TraceId/SpanId are set — SigNoz uses these for the
		//     Span Details "Logs" tab native trace-log correlation.
		spanCtx := c.Request.Context() // fallback: no span, but has request_id etc.
		if v, ok := c.Get(ginKeySpanCtx); ok {
			if savedCtx, ok := v.(context.Context); ok {
				spanCtx = savedCtx
			}
		}

		switch {
		case statusCode >= 500:
			errMsg := c.Errors.String()
			if errMsg == "" {
				errMsg = http.StatusText(statusCode)
			}
			log.Error(spanCtx, "HTTP_REQUEST_ERROR",
				"error", errMsg,
				"method", c.Request.Method,
				"path", path,
				"query", raw,
				"status", statusCode,
				"latency_ms", latency.Milliseconds(),
			)
		case statusCode >= 400:
			if len(c.Errors) > 0 {
				fields = append(fields, "errors", c.Errors.String())
			}
			log.Info(spanCtx, "HTTP_REQUEST_WARNING", fields...)
		default:
			// Successful requests are fully covered by tracing; keep at debug
			// to avoid flooding production log volume.
			log.Debug(spanCtx, "HTTP_REQUEST_INFO", fields...)
		}
	}
}
