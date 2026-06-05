package middleware

import (
	"bytes"
	"io"
	"strings"
	"time"
	"unicode/utf8"

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

const (
	// maxRequestBodyBytes is the upper bound on the http.request.body span
	// attribute. Bodies larger than this are truncated in the attribute, but the
	// handler always receives the complete body (up to maxBodyReadBytes).
	maxRequestBodyBytes = 8 * 1024 // 8 KB span attribute limit

	// maxBodyReadBytes is the maximum we buffer in memory per request. We read
	// up to this limit, restore the full bytes for the handler, and truncate only
	// the span attribute at maxRequestBodyBytes. Keeping a separate ceiling avoids
	// breaking handler body parsing when the body is between 8 KB and 80 KB.
	maxBodyReadBytes = 80 * 1024 // 80 KB read-buffer limit
)

// sensitivePathSegments lists URL path segments whose request bodies are never
// captured. These routes handle credentials, tokens, or other secrets.
var sensitivePathSegments = []string{
	"/auth/", "/login", "/signup", "/password", "/secret", "/token",
}

// captureableContentTypes lists content-type prefixes eligible for body capture.
// Binary, multipart, and streaming types are excluded because they are either
// unreadable as text or very large.
var captureableContentTypes = []string{
	"application/json",
	"application/x-www-form-urlencoded",
	"text/",
}

// SpanEnrichmentMiddleware runs after otelgin (which creates the span) and
// before the handler chain. It:
//  1. Pre-request: stamps app.request_id on the span for cross-signal searching.
//  2. Pre-request (optional): reads and re-buffers the request body, attaches
//     it as http.request.body on the span (truncated at 8 KB). Controlled by
//     cfg.Otel.Traces.CaptureRequestBody; skips auth/secret paths and
//     non-text content types.
//  3. Post-request: stashes trace_id/span_id and the full span-carrying context
//     into gin's key-value store while the span is still present, before
//     otelgin's deferred context-restore fires. LoggingMiddleware (post-phase
//     runs after otelgin fully returns) reads these to inject trace_id/span_id
//     into log fields and the OTLP log record — enabling SigNoz trace-log
//     correlation in the Span Details "Logs" tab.
//
// Note: otelgin v0.69.0 already handles 5xx span status marking and gin error
// recording. This middleware intentionally does NOT duplicate those operations.
//
// Middleware execution order:
//
//	Registration: [LoggingMW, otelgin, SpanEnrichmentMW, ...]
//	Pre-phase:    LoggingMW.pre → otelgin.pre (span created) → SpanEnrichment.pre
//	Post-phase:   SpanEnrichment.post → otelgin.post+defer (span ended, ctx restored) → LoggingMW.post
func SpanEnrichmentMiddleware(cfg *config.Configuration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
		span := trace.SpanFromContext(ctx)

		if span.SpanContext().IsValid() {
			// Pre-request: stamp request_id for cross-signal searching.
			if rid := types.GetRequestID(ctx); rid != "" {
				span.SetAttributes(attribute.String("app.request_id", rid))
			}

			// Pre-request: capture request body when enabled.
			if cfg.Otel.Traces.CaptureRequestBody {
				captureBody(c, span)
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

		// Note: otelgin v0.69.0 already handles 5xx status marking and gin error
		// recording. We do NOT duplicate those here to avoid double span events.
	}
}

// captureBody reads the request body, re-buffers it so the handler can still
// read it, and attaches it as http.request.body on the active span.
// Bodies larger than maxRequestBodyBytes are truncated. Sensitive paths and
// non-text content types are skipped.
func captureBody(c *gin.Context, span trace.Span) {
	path := c.Request.URL.Path

	// Skip sensitive paths — these carry credentials or tokens.
	for _, seg := range sensitivePathSegments {
		if strings.Contains(path, seg) {
			return
		}
	}

	// Skip when there is definitely no body.
	// ContentLength -1 means "unknown" (chunked/streaming) — still attempt capture.
	// ContentLength 0 means explicitly empty body.
	if c.Request.Body == nil || c.Request.ContentLength == 0 {
		return
	}
	if c.Request.ContentLength > int64(maxBodyReadBytes) {
		// Body is very large — skip to avoid significant memory overhead.
		span.SetAttributes(attribute.String("http.request.body", "[body too large to capture]"))
		return
	}

	// Only capture text-friendly content types.
	ct := strings.ToLower(c.Request.Header.Get("Content-Type"))
	// Empty content-type: assume JSON (common for API clients that omit it).
	if ct != "" {
		eligible := false
		for _, prefix := range captureableContentTypes {
			if strings.HasPrefix(ct, prefix) {
				eligible = true
				break
			}
		}
		if !eligible {
			return
		}
	}

	// Read up to maxBodyReadBytes. We restore ALL read bytes to the handler so
	// body parsing is never broken, even when the span attribute is truncated.
	// Without this, a LimitReader at 8 KB would feed only 8 KB back to the
	// handler for bodies between 8 KB and 80 KB, corrupting JSON parsing.
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(maxBodyReadBytes)+1))

	// Always restore whatever bytes were read, even on partial-read errors, so
	// the handler never receives an unexpectedly empty body.
	c.Request.Body = io.NopCloser(bytes.NewReader(body))

	if err != nil {
		// Partial read — skip setting the span attribute, body is restored above.
		return
	}

	if len(body) == 0 {
		return
	}

	// Truncate only the span attribute, not the body the handler receives.
	attrBody := body
	truncated := len(attrBody) > maxRequestBodyBytes
	if truncated {
		attrBody = attrBody[:maxRequestBodyBytes]
	}

	// Validate UTF-8 — OTel span attributes must be valid strings.
	if !utf8.Valid(attrBody) {
		span.SetAttributes(attribute.String("http.request.body", "[binary body omitted]"))
		return
	}

	bodyStr := string(attrBody)
	if truncated {
		bodyStr += " …[truncated]"
	}
	span.SetAttributes(attribute.String("http.request.body", bodyStr))
}

// TenantContextMiddleware enriches the active OTel span and Sentry scope with
// tenant_id, environment_id, and user_id. Mount this after AuthenticateMiddleware
// and EnvAccessMiddleware so the request context already has these set.
func TenantContextMiddleware(c *gin.Context) {
	ctx := c.Request.Context()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	// Attach tenant / user attributes to the OTel span.
	// Note: app.request_id is intentionally omitted here — SpanEnrichmentMiddleware
	// (which runs on all routes, including unauthenticated ones) already sets it in
	// its pre-phase, before this middleware runs.
	if span := trace.SpanFromContext(ctx); span != nil && span.SpanContext().IsValid() {
		attrs := make([]attribute.KeyValue, 0, 3)
		if tenantID != "" {
			attrs = append(attrs, attribute.String("app.tenant_id", tenantID))
		}
		if environmentID != "" {
			attrs = append(attrs, attribute.String("app.environment_id", environmentID))
		}
		if userID != "" {
			attrs = append(attrs, attribute.String("app.user_id", userID))
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
