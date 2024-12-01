package middleware

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// EnvironmentMiddleware is a middleware that sets the environment ID in the request context
// It expects the environment ID to be in the X-Environment-ID header
func EnvironmentMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Set additional headers for downstream handlers
		environmentID := c.GetHeader(types.HeaderEnvironment)

		if environmentID == "" {
			environmentID = types.DefaultEnvironmentID
		}

		ctx := context.WithValue(c.Request.Context(), types.CtxEnvironmentID, environmentID)
		c.Request = c.Request.WithContext(ctx)

		c.Next()
	}
}
