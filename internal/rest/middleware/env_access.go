package middleware

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// EnvAccessMiddleware checks if the user has access to the requested environment
func EnvAccessMiddleware(envAccessService service.EnvAccessService, logger *logger.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get user info from context
		userID := types.GetUserID(c.Request.Context())
		tenantID := types.GetTenantID(c.Request.Context())
		environmentID := types.GetEnvironmentID(c.Request.Context())

		// Skip check if no user is authenticated
		if userID == "" || tenantID == "" {
			c.Next()
			return
		}

		// Check if user has access to the environment
		hasAccess := envAccessService.HasEnvironmentAccess(c.Request.Context(), userID, tenantID, environmentID)
		if !hasAccess {
			logger.Info(c.Request.Context(), "environment access denied",
				"user_id", userID,
				"tenant_id", tenantID,
				"environment_id", environmentID,
			)
			c.JSON(http.StatusForbidden, gin.H{
				"error":   "Access denied",
				"message": "You don't have access to this environment",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
