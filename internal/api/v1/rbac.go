package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/rbac"
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
// @ID listRbacRoles
// @Description Use when building role pickers or permission UIs. Returns all roles with permissions and descriptions.
// @Tags RBAC
// @Accept json
// @Produce json
// @Param user_type query string false "Filter by user type" Enums(user, service_account)
// @Success 200 {object} map[string]interface{} "List of roles"
// @Failure 400 {object} ierr.ErrorResponse "Invalid request"
// @Failure 500 {object} ierr.ErrorResponse "Server error"
// @Router /rbac/roles [get]
// @Security ApiKeyAuth
func (h *RBACHandler) ListRoles(c *gin.Context) {
	var filter types.RoleFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation))
		return
	}

	// ShouldBindQuery maps query values via `form` tags and validates only
	// `binding` tags; RoleFilter.UserType has no binding rule for enum
	// values, so UserType.Validate() performs that check explicitly.
	if filter.UserType != nil {
		if err := filter.UserType.Validate(); err != nil {
			c.Error(err)
			return
		}
	}

	roles := h.rbacService.ListRoles(&filter)

	c.JSON(http.StatusOK, gin.H{
		"roles": roles,
	})
}

// GetRole returns a specific role by ID
// @Summary Get a specific RBAC role
// @ID getRbacRole
// @Description Use when you need to show or edit a single role (e.g. role detail page). Includes permissions, name, and description.
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
