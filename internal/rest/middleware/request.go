package middleware

import (
	"context"
	"regexp"
	"strings"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// bound the client request-id to a safe charset
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

func RequestIDMiddleware(c *gin.Context) {
	// Create a new context from the request context
	ctx := c.Request.Context()

	// reject unsafe or empty client values
	requestID := c.GetHeader("X-Request-ID")
	if !requestIDPattern.MatchString(requestID) {
		requestID = types.GenerateUUID()
	}

	// Create new context with values
	ctx = context.WithValue(ctx, types.CtxRequestID, requestID)

	// Replace request context
	c.Request = c.Request.WithContext(ctx)

	// Add headers for response
	c.Header(types.HeaderRequestID, requestID)

	c.Next()
}

// RequestMetaMiddleware records the client IP and user agent for activity entries.
func RequestMetaMiddleware(c *gin.Context) {
	ctx := types.SetRequestMeta(c.Request.Context(), c.ClientIP(), c.Request.UserAgent())
	c.Request = c.Request.WithContext(ctx)
	c.Next()
}

// WebhookActorMiddleware attributes changes made by an inbound webhook to the provider named in the route.
func WebhookActorMiddleware(c *gin.Context) {
	provider := strings.TrimPrefix(c.FullPath(), "/v1/webhooks/")
	if i := strings.IndexByte(provider, '/'); i >= 0 {
		provider = provider[:i]
	}
	actor := types.Actor{Type: types.ActorTypeSystem, ID: provider}
	c.Request = c.Request.WithContext(types.SetActor(c.Request.Context(), actor, types.SourceWebhook))
	c.Next()
}
