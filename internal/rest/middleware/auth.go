package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/flexprice/flexprice/internal/auth"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// GuestAuthenticateMiddleware is a middleware that allows requests without authentication
// For now it sets a default tenant ID and user ID in the request context
// TODO: Implement guest user id logic if needed
func GuestAuthenticateMiddleware(c *gin.Context) {
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, types.CtxTenantID, types.DefaultTenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, types.DefaultUserID)
	c.Request = c.Request.WithContext(ctx)
	c.Next()
}

// AuthenticateMiddleware is a middleware that authenticates requests based on the JWT token
// It expects the JWT token to be in the Authorization header as a Bearer token
// It sets the user ID and JWT token in the request context so it can be used by downstream handlers
// It also sets the environment ID and other headers in the request context if present
func AuthenticateMiddleware(cfg *config.Configuration, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader(types.HeaderAuthorization)
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
			c.Abort()
			return
		}

		// Check if the authorization header is in the correct format
		if !strings.HasPrefix(authHeader, "Bearer ") {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization header format"})
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")

		provider := auth.NewProvider(cfg)

		claims, err := provider.ValidateToken(c.Request.Context(), tokenString)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token: " + err.Error()})
			c.Abort()
			return
		}

		if claims == nil || claims.UserID == "" || claims.TenantID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}

		ctx := c.Request.Context()
		ctx = context.WithValue(ctx, types.CtxUserID, claims.UserID)
		ctx = context.WithValue(ctx, types.CtxTenantID, claims.TenantID)
		ctx = context.WithValue(ctx, types.CtxJWT, tokenString)
		c.Request = c.Request.WithContext(ctx)

		logger.Debugf("authenticated request: user_id=%s, tenant_id=%s", claims.UserID, claims.TenantID)
		c.Next()
	}
}
