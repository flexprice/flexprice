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

// validateAPIKey validates the API key and returns tenant ID and user ID if valid
func validateAPIKey(cfg *config.Configuration, apiKey string) (tenantID, userID string, valid bool) {
	if apiKey == "" {
		return "", "", false
	}
	return auth.ValidateAPIKey(cfg, apiKey)
}

// setContextValues sets the tenant ID and user ID in the context
func setContextValues(c *gin.Context, tenantID, userID string) {
	ctx := c.Request.Context()
	ctx = context.WithValue(ctx, types.CtxTenantID, tenantID)
	ctx = context.WithValue(ctx, types.CtxUserID, userID)

	// Set additional headers for downstream handlers
	environmentID := c.GetHeader(types.HeaderEnvironment)
	if environmentID != "" {
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, environmentID)
	}
	c.Request = c.Request.WithContext(ctx)
}

// GuestAuthenticateMiddleware is a middleware that allows requests without authentication
// For now it sets a default tenant ID and user ID in the request context
func GuestAuthenticateMiddleware(c *gin.Context) {
	setContextValues(c, types.DefaultTenantID, types.DefaultUserID)
	c.Next()
}

// APIKeyAuthMiddleware is a middleware that only allows requests with valid API keys
func APIKeyAuthMiddleware(cfg *config.Configuration, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader(cfg.Auth.APIKey.Header)
		tenantID, userID, valid := validateAPIKey(cfg, apiKey)
		if !valid {
			logger.Debugw("invalid api key")
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid API key"})
			c.Abort()
			return
		}

		setContextValues(c, tenantID, userID)
		c.Next()
	}
}

// AuthenticateMiddleware is a middleware that authenticates requests based on either:
// 1. JWT token in the Authorization header as a Bearer token
// 2. API key in the x-api-key header (or configured header name)
func AuthenticateMiddleware(cfg *config.Configuration, logger *logger.Logger) gin.HandlerFunc {
	authProvider := auth.NewProvider(cfg)

	return func(c *gin.Context) {
		// First check for API key
		apiKey := c.GetHeader(cfg.Auth.APIKey.Header)
		tenantID, userID, valid := validateAPIKey(cfg, apiKey)
		if valid {
			setContextValues(c, tenantID, userID)
			c.Next()
			return
		}

		// If no API key, check for JWT token
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
		claims, err := authProvider.ValidateToken(c.Request.Context(), tokenString)
		if err != nil {
			logger.Errorw("failed to validate token", "error", err)
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token"})
			c.Abort()
			return
		}

		if claims == nil || claims.UserID == "" || claims.TenantID == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid token claims"})
			c.Abort()
			return
		}

		setContextValues(c, claims.TenantID, claims.UserID)
		c.Next()
	}
}
