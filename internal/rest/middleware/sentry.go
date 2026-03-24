package middleware

import (
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

// SentryMiddleware returns a middleware that captures errors and performance data
func SentryMiddleware(cfg *config.Configuration) gin.HandlerFunc {
	if !cfg.Sentry.Enabled {
		return func(c *gin.Context) {
			c.Next()
		}
	}

	return sentrygin.New(sentrygin.Options{
		Repanic:         true,
		WaitForDelivery: false,
		Timeout:         2 * time.Second,
	})
}

// SentryTenantContextMiddleware sets tenant_id, environment_id, and user_id on the Sentry scope
// when they are present in the request context (e.g. after auth). Add this after
// AuthenticateMiddleware and EnvAccessMiddleware so the auto-captured span includes
// these tags for private routes.
func SentryTenantContextMiddleware(c *gin.Context) {
	hub := sentrygin.GetHubFromContext(c)
	if hub == nil {
		c.Next()
		return
	}
	ctx := c.Request.Context()
	if tenantID := types.GetTenantID(ctx); tenantID != "" {
		hub.Scope().SetTag("tenant_id", tenantID)
	}
	if environmentID := types.GetEnvironmentID(ctx); environmentID != "" {
		hub.Scope().SetTag("environment_id", environmentID)
	}
	if userID := types.GetUserID(ctx); userID != "" {
		hub.Scope().SetTag("user_id", userID)
		hub.Scope().SetUser(sentry.User{ID: userID})
	}
	c.Next()
}
