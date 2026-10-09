package middleware

import (
	"context"
	"regexp"

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
