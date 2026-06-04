package middleware

import (
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware is the combined HTTP tracing + error capture middleware.
// It uses otelgin to create a span per request (exported to SigNoz / any OTLP
// backend) and sentrygin for panic recovery + scope binding into Sentry Issues.
func TracingMiddleware(cfg *config.Configuration) []gin.HandlerFunc {
	handlers := []gin.HandlerFunc{}

	if cfg.Otel.Enabled && cfg.Otel.Traces.Enabled {
		handlers = append(handlers, otelgin.Middleware(cfg.Otel.ResolveServiceName(cfg)))
	}

	if cfg.Sentry.Enabled {
		handlers = append(handlers, sentrygin.New(sentrygin.Options{
			Repanic:         true,
			WaitForDelivery: false,
			Timeout:         2 * time.Second,
		}))
	}

	return handlers
}

// Gin context keys used to propagate OTel trace context from SpanEnrichmentMiddleware
// to LoggingMiddleware. otelgin's deferred context-restore fires between the two
// post-phases, so we stash the span IDs and the pre-restore request context here.
const (
	ginKeyTraceID  = "_otel_trace_id"
	ginKeySpanID   = "_otel_span_id"
	ginKeySpanCtx  = "_otel_span_ctx" // full context.Context with active span
)

// SpanEnrichmentMiddleware runs after otelgin (which creates the span) and
// before the handler chain. It:
//  1. Pre-request: stamps request_id on the span.
//  2. Post-request: stashes trace_id/span_id in gin's context while the span
//     is still present, before otelgin's deferred context-restore fires. This
//     allows LoggingMiddleware (post-phase runs after otelgin fully returns) to
//     read the IDs via c.GetString(ginKeyTraceID) without depending on the span
//     still being in c.Request.Context() — which otelgin removes via defer.
//  3. Marks the span as ERROR for 5xx responses and records gin errors as span events.
//
// Middleware execution order:
//
//	Registration: [LoggingMW, otelgin, SpanEnrichmentMW, ...]
//	Pre-phase:    LoggingMW.pre → otelgin.pre (span created) → SpanEnrichment.pre
//	Post-phase:   SpanEnrichment.post → otelgin.post+defer (span ended, ctx restored) → LoggingMW.post
func SpanEnrichmentMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)

		// Pre-request: attach request_id to the span for cross-signal searching.
		if span.SpanContext().IsValid() {
			if rid := types.GetRequestID(ctx); rid != "" {
				span.SetAttributes(attribute.String("app.request_id", rid))
			}
		}

		c.Next()

		// Post-request: the span is still in c.Request.Context() here because
		// otelgin's deferred restore hasn't fired yet (it fires when otelgin's
		// function returns, which is after our post-phase completes).
		sc := trace.SpanFromContext(c.Request.Context()).SpanContext()
		if sc.IsValid() {
			// Stash IDs and the full span-carrying context in gin's key-value store.
			// After otelgin's deferred restore fires (which removes the span from
			// c.Request.Context()), LoggingMiddleware reads these to:
			//   1. Inject trace_id/span_id as string log fields (stdout + OTLP attributes).
			//   2. Pass ginKeySpanCtx to logger.WithContext so the otelzap bridge can set
			//      the OTLP log record's record-level TraceId/SpanId — the fields SigNoz
			//      uses to link logs to their trace in the Span Details "Logs" tab.
			c.Set(ginKeyTraceID, sc.TraceID().String())
			c.Set(ginKeySpanID, sc.SpanID().String())
			c.Set(ginKeySpanCtx, c.Request.Context()) // save ctx BEFORE otelgin restores it
		}

		// Note: otelgin v0.69.0 already:
		//   - calls span.SetStatus(sc.Status(status)) which marks 5xx as codes.Error
		//   - iterates c.Errors and calls span.RecordError for each
		// We intentionally do NOT duplicate those here to avoid double span events.
		// SpanEnrichmentMiddleware's sole additional responsibility is:
		//   1. app.request_id attribute (set in the pre-phase above)
		//   2. Stashing the span context for LoggingMiddleware (done above)
	}
}

// TenantContextMiddleware enriches the active OTel span and Sentry scope with
// tenant_id, environment_id, and user_id. Mount this after AuthenticateMiddleware
// and EnvAccessMiddleware so the request context already has these set.
func TenantContextMiddleware(c *gin.Context) {
	ctx := c.Request.Context()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)
	requestID := types.GetRequestID(ctx)

	// Attach to OTel span — visible as searchable attributes in SigNoz trace explorer.
	if span := trace.SpanFromContext(ctx); span != nil && span.SpanContext().IsValid() {
		attrs := make([]attribute.KeyValue, 0, 4)
		if tenantID != "" {
			attrs = append(attrs, attribute.String("app.tenant_id", tenantID))
		}
		if environmentID != "" {
			attrs = append(attrs, attribute.String("app.environment_id", environmentID))
		}
		if userID != "" {
			attrs = append(attrs, attribute.String("app.user_id", userID))
		}
		if requestID != "" {
			attrs = append(attrs, attribute.String("app.request_id", requestID))
		}
		if len(attrs) > 0 {
			span.SetAttributes(attrs...)
		}
	}

	// Attach to Sentry scope (visible in Sentry Issues)
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		if tenantID != "" {
			hub.Scope().SetTag("tenant_id", tenantID)
		}
		if environmentID != "" {
			hub.Scope().SetTag("environment_id", environmentID)
		}
		if userID != "" {
			hub.Scope().SetTag("user_id", userID)
			hub.Scope().SetUser(sentry.User{ID: userID})
		}
	}

	c.Next()
}
