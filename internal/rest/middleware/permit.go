package middleware

import (
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type PermitMiddleware struct {
	permitService service.PermitInterface
	logger        *logger.Logger
}

func NewPermitMiddleware(permitService service.PermitInterface, logger *logger.Logger) *PermitMiddleware {
	return &PermitMiddleware{
		permitService: permitService,
		logger:        logger,
	}
}

// RequirePermission middleware checks if user has permission for the endpoint
func (pm *PermitMiddleware) RequirePermission(action, resource string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract user info from context
		userID := types.GetUserID(c.Request.Context())
		if userID == "" {
			if err := c.Error(ierr.NewError("user not found in context").
				Mark(ierr.ErrPermissionDenied)); err != nil {
				pm.logger.Errorw("failed to set error in context", "error", err)
			}
			c.Abort()
			return
		}

		tenantID := types.GetTenantID(c.Request.Context())
		if tenantID == "" {
			if err := c.Error(ierr.NewError("tenant not found in context").
				Mark(ierr.ErrPermissionDenied)); err != nil {
				pm.logger.Errorw("failed to set error in context", "error", err)
			}
			c.Abort()
			return
		}

		// Check if tenant has RBAC enabled
		rbacEnabled, err := pm.permitService.IsTenantRBACEnabled(c.Request.Context(), tenantID)

		fmt.Println("rbacEnabled", rbacEnabled)
		if err != nil {
			pm.logger.Warnw("failed to check tenant RBAC status, allowing access",
				"tenant_id", tenantID,
				"user_id", userID,
				"error", err)
			c.Next()
			return
		}

		// If tenant doesn't have RBAC enabled, allow access
		if !rbacEnabled {
			pm.logger.Debugw("tenant RBAC not enabled, allowing access",
				"tenant_id", tenantID,
				"user_id", userID,
				"action", action,
				"resource", resource)
			c.Next()
			return
		}

		// RBAC is enabled, perform permission check
		allowed, err := pm.permitService.CheckPermission(
			c.Request.Context(),
			userID,
			action,
			resource,
			tenantID,
		)
		if err != nil {
			// Permission check failed - allow access and log
			pm.logger.Warnw("permission check failed in permit.io, allowing access",
				"tenant_id", tenantID,
				"user_id", userID,
				"action", action,
				"resource", resource,
				"error", err)
			c.Next()
			return
		}

		if !allowed {
			// Permission denied - log and block access
			pm.logger.Warnw("permission denied",
				"tenant_id", tenantID,
				"user_id", userID,
				"action", action,
				"resource", resource,
				"allowed", allowed)
			if err := c.Error(ierr.NewError("permission denied").
				WithHint("User does not have required permission").
				WithReportableDetails(map[string]interface{}{
					"user_id":   userID,
					"action":    action,
					"resource":  resource,
					"tenant_id": tenantID,
				}).
				Mark(ierr.ErrPermissionDenied)); err != nil {
				pm.logger.Errorw("failed to set error in context", "error", err)
			}
			c.Abort()
			return
		}

		c.Next()
	}
}

// RequirePermissionWithAttributes middleware checks permission with additional attributes
func (pm *PermitMiddleware) RequirePermissionWithAttributes(action, resource string, attributeExtractor func(c *gin.Context) map[string]interface{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Extract user info from context
		userID := types.GetUserID(c.Request.Context())
		if userID == "" {
			if err := c.Error(ierr.NewError("user not found in context").
				Mark(ierr.ErrPermissionDenied)); err != nil {
				pm.logger.Errorw("failed to set error in context", "error", err)
			}
			c.Abort()
			return
		}

		tenantID := types.GetTenantID(c.Request.Context())
		if tenantID == "" {
			if err := c.Error(ierr.NewError("tenant not found in context").
				Mark(ierr.ErrPermissionDenied)); err != nil {
				pm.logger.Errorw("failed to set error in context", "error", err)
			}
			c.Abort()
			return
		}

		// Check if tenant has RBAC enabled
		rbacEnabled, err := pm.permitService.IsTenantRBACEnabled(c.Request.Context(), tenantID)
		if err != nil {
			pm.logger.Warnw("failed to check tenant RBAC status, allowing access",
				"tenant_id", tenantID,
				"user_id", userID,
				"error", err)
			c.Next()
			return
		}

		// If tenant doesn't have RBAC enabled, allow access
		if !rbacEnabled {
			pm.logger.Debugw("tenant RBAC not enabled, allowing access",
				"tenant_id", tenantID,
				"user_id", userID,
				"action", action,
				"resource", resource)
			c.Next()
			return
		}

		// Extract attributes
		attributes := attributeExtractor(c)

		// RBAC is enabled, perform permission check with attributes
		allowed, err := pm.permitService.CheckPermissionWithAttributes(
			c.Request.Context(),
			userID,
			action,
			resource,
			attributes,
		)
		if err != nil {
			if err := c.Error(ierr.WithError(err).
				WithHint("Failed to check permission").
				Mark(ierr.ErrPermissionDenied)); err != nil {
				pm.logger.Errorw("failed to set error in context", "error", err)
			}
			c.Abort()
			return
		}

		if !allowed {
			if err := c.Error(ierr.NewError("permission denied").
				WithHint("User does not have required permission").
				WithReportableDetails(map[string]interface{}{
					"user_id":    userID,
					"action":     action,
					"resource":   resource,
					"attributes": attributes,
				}).
				Mark(ierr.ErrPermissionDenied)); err != nil {
				pm.logger.Errorw("failed to set error in context", "error", err)
			}
			c.Abort()
			return
		}

		c.Next()
	}
}
