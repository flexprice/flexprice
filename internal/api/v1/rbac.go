package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
	"github.com/flexprice/flexprice/internal/service"
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
// @Description Returns all available roles with their permissions, names, and descriptions
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
