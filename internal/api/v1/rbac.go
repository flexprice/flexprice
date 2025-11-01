package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

type RBACHandler struct {
	rbacService *rbac.RBACService
	userService service.UserService
	logger      *logger.Logger
}

func NewRBACHandler(rbacService *rbac.RBACService, userService service.UserService, logger *logger.Logger) *RBACHandler {
	return &RBACHandler{
		rbacService: rbacService,
		userService: userService,
		logger:      logger,
	}
}

// ListRoles returns all available roles with their metadata
// @Summary List all RBAC roles
// @Description Returns all available roles with their permissions, names, and descriptions for UI consumption
// @Tags RBAC
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "List of roles"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /rbac/roles [get]
// @Security ApiKeyAuth
func (h *RBACHandler) ListRoles(c *gin.Context) {
	roles := h.rbacService.ListRoles()

	c.JSON(http.StatusOK, gin.H{
		"roles": roles,
	})
}

// GetRole returns a specific role by ID
// @Summary Get a specific RBAC role
// @Description Returns details of a specific role including permissions, name, and description
// @Tags RBAC
// @Accept json
// @Produce json
// @Param id path string true "Role ID"
// @Success 200 {object} map[string]interface{} "Role details"
// @Failure 404 {object} map[string]string "Role not found"
// @Router /rbac/roles/{id} [get]
// @Security ApiKeyAuth
func (h *RBACHandler) GetRole(c *gin.Context) {
	roleID := c.Param("id")

	role, exists := h.rbacService.GetRole(roleID)
	if !exists {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Role not found",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"role": role,
	})
}

// ListServiceAccounts returns all service accounts
// @Summary List all service accounts
// @Description Returns all users with type 'service_account'
// @Tags RBAC
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "List of service accounts"
// @Failure 500 {object} map[string]string "Internal server error"
// @Router /users/service-accounts [get]
// @Security ApiKeyAuth
func (h *RBACHandler) ListServiceAccounts(c *gin.Context) {
	// Get tenant ID from request context (set by auth middleware)
	tenantID := types.GetTenantID(c.Request.Context())
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "tenant_id not found in context",
		})
		return
	}

	// List users filtered by type=service_account
	users, err := h.userService.ListServiceAccounts(c.Request.Context(), tenantID)
	if err != nil {
		h.logger.Error("Failed to list service accounts", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Failed to list service accounts",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_accounts": users,
	})
}

// GetServiceAccount returns a specific service account
// @Summary Get a specific service account
// @Description Returns details of a specific service account
// @Tags RBAC
// @Accept json
// @Produce json
// @Param id path string true "Service Account ID"
// @Success 200 {object} map[string]interface{} "Service account details"
// @Failure 404 {object} map[string]string "Service account not found"
// @Router /users/service-accounts/{id} [get]
// @Security ApiKeyAuth
func (h *RBACHandler) GetServiceAccount(c *gin.Context) {
	userID := c.Param("id")

	// Get tenant ID from request context (set by auth middleware)
	tenantID := types.GetTenantID(c.Request.Context())
	if tenantID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "tenant_id not found in context",
		})
		return
	}

	// Get user
	user, err := h.userService.GetByID(c.Request.Context(), userID, tenantID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "Service account not found",
		})
		return
	}

	// Verify it's a service account
	if user.Type != "service_account" {
		c.JSON(http.StatusNotFound, gin.H{
			"error": "User is not a service account",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"service_account": user,
	})
}
