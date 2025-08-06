package v1

import (
	"net/http"
	"strconv"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/auth/rbac"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/gin-gonic/gin"
)

// RBACHandler handles RBAC-related HTTP requests
type RBACHandler struct {
	rbacService *rbac.Service
	logger      *logger.Logger
}

// NewRBACHandler creates a new RBAC handler
func NewRBACHandler(rbacService *rbac.Service, logger *logger.Logger) *RBACHandler {
	return &RBACHandler{
		rbacService: rbacService,
		logger:      logger,
	}
}

// AssignRole handles role assignment requests
func (h *RBACHandler) AssignRole(c *gin.Context) {
	var req dto.RoleAssignmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current user and tenant from context
	userID := types.GetUserID(c.Request.Context())
	tenantID := types.GetTenantID(c.Request.Context())

	// Check if current user has permission to assign roles
	allowed, err := h.rbacService.CheckPermission(c.Request.Context(), userID, "user", "update", tenantID)
	if err != nil {
		h.logger.Errorw("failed to check permission for role assignment", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to assign roles"})
		return
	}

	// Assign role
	err = h.rbacService.AssignRole(c.Request.Context(), req.UserID, req.Role, tenantID)
	if err != nil {
		h.logger.Errorw("failed to assign role", "error", err, "user_id", req.UserID, "role", req.Role)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to assign role"})
		return
	}

	response := dto.RoleAssignmentResponse{
		UserID:   req.UserID,
		Role:     req.Role,
		TenantID: tenantID,
		Message:  "Role assigned successfully",
	}

	c.JSON(http.StatusOK, response)
}

// RemoveRole handles role removal requests
func (h *RBACHandler) RemoveRole(c *gin.Context) {
	var req dto.RoleAssignmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current user and tenant from context
	userID := types.GetUserID(c.Request.Context())
	tenantID := types.GetTenantID(c.Request.Context())

	// Check if current user has permission to remove roles
	allowed, err := h.rbacService.CheckPermission(c.Request.Context(), userID, "user", "update", tenantID)
	if err != nil {
		h.logger.Errorw("failed to check permission for role removal", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to remove roles"})
		return
	}

	// Remove role
	err = h.rbacService.RemoveRole(c.Request.Context(), req.UserID, req.Role, tenantID)
	if err != nil {
		h.logger.Errorw("failed to remove role", "error", err, "user_id", req.UserID, "role", req.Role)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove role"})
		return
	}

	response := dto.RoleAssignmentResponse{
		UserID:   req.UserID,
		Role:     req.Role,
		TenantID: tenantID,
		Message:  "Role removed successfully",
	}

	c.JSON(http.StatusOK, response)
}

// GetUserRoles handles requests to get user roles
func (h *RBACHandler) GetUserRoles(c *gin.Context) {
	userID := c.Param("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID is required"})
		return
	}

	// Get current user and tenant from context
	currentUserID := types.GetUserID(c.Request.Context())
	tenantID := types.GetTenantID(c.Request.Context())

	// Check if current user has permission to view user roles
	allowed, err := h.rbacService.CheckPermission(c.Request.Context(), currentUserID, "user", "read", tenantID)
	if err != nil {
		h.logger.Errorw("failed to check permission for viewing user roles", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to view user roles"})
		return
	}

	// Get user roles
	roles, err := h.rbacService.GetUserRoles(c.Request.Context(), userID, tenantID)
	if err != nil {
		h.logger.Errorw("failed to get user roles", "error", err, "user_id", userID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get user roles"})
		return
	}

	response := dto.UserRolesResponse{
		UserID: userID,
		Roles:  roles,
	}

	c.JSON(http.StatusOK, response)
}

// CheckAuthorization handles authorization check requests
func (h *RBACHandler) CheckAuthorization(c *gin.Context) {
	var req dto.AuthorizationCheckRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current user and tenant from context
	userID := types.GetUserID(c.Request.Context())
	tenantID := types.GetTenantID(c.Request.Context())

	// Check permission
	allowed, err := h.rbacService.CheckPermission(c.Request.Context(), userID, req.Resource, req.Action, tenantID)
	if err != nil {
		h.logger.Errorw("failed to check authorization", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	response := dto.AuthorizationCheckResponse{
		Allowed: allowed,
		Reason:  "Permission check completed",
	}

	c.JSON(http.StatusOK, response)
}

// CreateRole handles requests to create a new role
func (h *RBACHandler) CreateRole(c *gin.Context) {
	var req dto.CreateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current user and tenant from context
	userID := types.GetUserID(c.Request.Context())
	tenantID := types.GetTenantID(c.Request.Context())

	// Check if current user has permission to create roles
	allowed, err := h.rbacService.CheckPermission(c.Request.Context(), userID, "role", "create", tenantID)
	if err != nil {
		h.logger.Errorw("failed to check permission for role creation", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to create roles"})
		return
	}

	// Create role and its policies
	err = h.rbacService.CreateRole(c.Request.Context(), req.Name, req.Description, req.Permissions, tenantID)
	if err != nil {
		h.logger.Errorw("failed to create role", "error", err, "role_name", req.Name)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create role"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"message": "Role created successfully"})
}

// UpdateRole handles requests to update an existing role
func (h *RBACHandler) UpdateRole(c *gin.Context) {
	roleID := c.Param("role_id")
	if roleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role ID is required"})
		return
	}

	var req dto.UpdateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get current user and tenant from context
	userID := types.GetUserID(c.Request.Context())
	tenantID := types.GetTenantID(c.Request.Context())

	// Check if current user has permission to update roles
	allowed, err := h.rbacService.CheckPermission(c.Request.Context(), userID, "role", "update", tenantID)
	if err != nil {
		h.logger.Errorw("failed to check permission for role update", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to update roles"})
		return
	}

	// Update role
	err = h.rbacService.UpdateRole(c.Request.Context(), roleID, req.Name, req.Description, req.Permissions, tenantID)
	if err != nil {
		h.logger.Errorw("failed to update role", "error", err, "role_id", roleID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update role"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Role updated successfully"})
}

// DeleteRole handles requests to delete a role
func (h *RBACHandler) DeleteRole(c *gin.Context) {
	roleID := c.Param("role_id")
	if roleID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Role ID is required"})
		return
	}

	// Get current user and tenant from context
	userID := types.GetUserID(c.Request.Context())
	tenantID := types.GetTenantID(c.Request.Context())

	// Check if current user has permission to delete roles
	allowed, err := h.rbacService.CheckPermission(c.Request.Context(), userID, "role", "delete", tenantID)
	if err != nil {
		h.logger.Errorw("failed to check permission for role deletion", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to delete roles"})
		return
	}

	// Delete role
	err = h.rbacService.DeleteRole(c.Request.Context(), roleID, tenantID)
	if err != nil {
		h.logger.Errorw("failed to delete role", "error", err, "role_id", roleID)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to delete role"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Role deleted successfully"})
}

// ListRoles handles requests to list all roles
func (h *RBACHandler) ListRoles(c *gin.Context) {
	// Get current user and tenant from context
	userID := types.GetUserID(c.Request.Context())
	tenantID := types.GetTenantID(c.Request.Context())

	// Check if current user has permission to list roles
	allowed, err := h.rbacService.CheckPermission(c.Request.Context(), userID, "role", "read", tenantID)
	if err != nil {
		h.logger.Errorw("failed to check permission for listing roles", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	if !allowed {
		c.JSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions to list roles"})
		return
	}

	// Get pagination parameters
	limitStr := c.DefaultQuery("limit", "10")
	offsetStr := c.DefaultQuery("offset", "0")

	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit parameter"})
		return
	}

	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid offset parameter"})
		return
	}

	// List roles
	roles, total, err := h.rbacService.ListRoles(c.Request.Context(), tenantID, limit, offset)
	if err != nil {
		h.logger.Errorw("failed to list roles", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list roles"})
		return
	}

	response := dto.ListRolesResponse{
		Roles: roles,
		Total: total,
	}

	c.JSON(http.StatusOK, response)
}
