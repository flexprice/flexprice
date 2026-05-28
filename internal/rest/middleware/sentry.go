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

// SentryMiddleware is retained as a thin wrapper around TracingMiddleware so
// the router doesn't have to know about the slice form. Prefer TracingMiddleware
// in new code.
//
// Deprecated: kept for back-compat; new code should use TracingMiddleware.
func SentryMiddleware(cfg *config.Configuration) []gin.HandlerFunc {
	return TracingMiddleware(cfg)
}

// TenantContextMiddleware enriches the active OTel span and Sentry scope with
// tenant_id, environment_id, and user_id. Mount this after AuthenticateMiddleware
// and EnvAccessMiddleware so the request context already has these set.
func TenantContextMiddleware(c *gin.Context) {
	ctx := c.Request.Context()

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	userID := types.GetUserID(ctx)

	// Attach to OTel span (visible in SigNoz)
	if span := trace.SpanFromContext(ctx); span != nil && span.SpanContext().IsValid() {
		if tenantID != "" {
			span.SetAttributes(attribute.String("tenant_id", tenantID))
		}
		if environmentID != "" {
			span.SetAttributes(attribute.String("environment_id", environmentID))
		}
		if userID != "" {
			span.SetAttributes(attribute.String("user_id", userID))
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

// SentryTenantContextMiddleware is the previous name for TenantContextMiddleware.
//
// Deprecated: use TenantContextMiddleware.
func SentryTenantContextMiddleware(c *gin.Context) {
	TenantContextMiddleware(c)
}
