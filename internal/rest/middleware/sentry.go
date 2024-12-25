package middleware

import (
	"time"

	"github.com/flexprice/flexprice/internal/config"
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
