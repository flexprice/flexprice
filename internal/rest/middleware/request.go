package middleware

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

func RequestIDMiddleware(c *gin.Context) {
	// Create a new context from the request context
	ctx := c.Request.Context()

	// Add request ID
	requestID := c.GetHeader("X-Request-ID")
	if requestID == "" {
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
