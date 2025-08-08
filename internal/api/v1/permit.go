package v1

import (
	"net/http"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/gin-gonic/gin"
)

type PermitHandler struct {
	permitService service.PermitInterface
}

func NewPermitHandler(permitService service.PermitInterface) *PermitHandler {
	return &PermitHandler{
		permitService: permitService,
	}
}

// @Summary Create a new tenant in Permit.io
// @Description Create a new tenant for authorization management
// @Tags Permit
// @Accept json
// @Produce json
// @Param request body dto.CreatePermitTenantRequest true "Create tenant request"
// @Success 200 {object} dto.CreatePermitTenantResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /permit/tenants [post]
func (h *PermitHandler) CreateTenant(c *gin.Context) {
	var req dto.CreatePermitTenantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.permitService.CreateTenant(c.Request.Context(), req.TenantID, req.Name)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.CreatePermitTenantResponse{
		Message:  "Tenant created successfully",
		TenantID: req.TenantID,
		Name:     req.Name,
	})
}

// @Summary Sync user to Permit.io
// @Description Sync a user to Permit.io with tenant association for authorization
// @Tags Permit
// @Accept json
// @Produce json
// @Param request body dto.SyncUserRequest true "Sync user request"
// @Success 200 {object} dto.SyncUserResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /permit/users/sync [post]
func (h *PermitHandler) SyncUser(c *gin.Context) {
	var req dto.SyncUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.permitService.SyncUser(c.Request.Context(), req.UserID, req.Email, req.TenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.SyncUserResponse{
		Message: "User synced successfully",
		UserID:  req.UserID,
	})
}

// @Summary Get tenant roles
// @Description Get all roles available in a tenant
// @Tags Permit
// @Accept json
// @Produce json
// @Param tenant_id query string true "Tenant ID"
// @Success 200 {object} dto.GetTenantRolesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /permit/tenants/roles [get]
func (h *PermitHandler) GetTenantRoles(c *gin.Context) {
	tenantID := c.Query("tenant_id")
	if tenantID == "" {
		c.Error(ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	roles, err := h.permitService.GetTenantRoles(c.Request.Context(), tenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.GetTenantRolesResponse{
		Message:  "Tenant roles retrieved successfully",
		TenantID: tenantID,
		Roles:    roles,
	})
}

// @Summary Get tenant resources
// @Description Get all resources available in a tenant
// @Tags Permit
// @Accept json
// @Produce json
// @Param tenant_id query string true "Tenant ID"
// @Success 200 {object} dto.GetTenantResourcesResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /permit/tenants/resources [get]
func (h *PermitHandler) GetTenantResources(c *gin.Context) {
	tenantID := c.Query("tenant_id")
	if tenantID == "" {
		c.Error(ierr.NewError("tenant_id is required").
			WithHint("Tenant ID is required").
			Mark(ierr.ErrValidation))
		return
	}

	resources, err := h.permitService.GetTenantResources(c.Request.Context(), tenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.GetTenantResourcesResponse{
		Message:   "Tenant resources retrieved successfully",
		TenantID:  tenantID,
		Resources: resources,
	})
}

// @Summary Assign role to user
// @Description Assign a role to a user in Permit.io
// @Tags Permit
// @Accept json
// @Produce json
// @Param request body dto.AssignRoleRequest true "Assign role request"
// @Success 200 {object} dto.AssignRoleResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /permit/users/assign-role [post]
func (h *PermitHandler) AssignRole(c *gin.Context) {
	var req dto.AssignRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.permitService.AssignRole(c.Request.Context(), req.UserID, req.RoleKey, req.TenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.AssignRoleResponse{
		Message: "Role assigned successfully",
		UserID:  req.UserID,
		RoleKey: req.RoleKey,
	})
}

// @Summary Create a new role
// @Description Create a new role in Permit.io
// @Tags Permit
// @Accept json
// @Produce json
// @Param request body dto.CreateRoleRequest true "Create role request"
// @Success 200 {object} dto.CreateRoleResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /permit/roles [post]
func (h *PermitHandler) CreateRole(c *gin.Context) {
	var req dto.CreateRoleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	err := h.permitService.CreateRole(c.Request.Context(), req.RoleKey, req.Name, req.Description, req.TenantID, req.Permissions)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.CreateRoleResponse{
		Message:  "Role created successfully",
		RoleKey:  req.RoleKey,
		Name:     req.Name,
		TenantID: req.TenantID,
	})
}

// @Summary Check user permission
// @Description Check if a user has permission to perform an action on a resource
// @Tags Permit
// @Accept json
// @Produce json
// @Param request body dto.CheckPermissionRequest true "Check permission request"
// @Success 200 {object} dto.CheckPermissionResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /permit/check-permission [post]
func (h *PermitHandler) CheckPermission(c *gin.Context) {
	var req dto.CheckPermissionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	allowed, err := h.permitService.CheckPermission(c.Request.Context(), req.UserID, req.Action, req.Resource, req.TenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.CheckPermissionResponse{
		Allowed:  allowed,
		UserID:   req.UserID,
		Action:   req.Action,
		Resource: req.Resource,
	})
}

// @Summary Get user permissions
// @Description Get all permissions for a user within a tenant
// @Tags Permit
// @Accept json
// @Produce json
// @Param request body dto.GetUserPermissionsRequest true "Get user permissions request"
// @Success 200 {object} dto.GetUserPermissionsResponse
// @Failure 400 {object} ierr.ErrorResponse
// @Router /permit/users/permissions [post]
func (h *PermitHandler) GetUserPermissions(c *gin.Context) {
	var req dto.GetUserPermissionsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(ierr.WithError(err).
			WithHint("Please check the request payload").
			Mark(ierr.ErrValidation))
		return
	}

	permissions, err := h.permitService.GetUserPermissions(c.Request.Context(), req.UserID, req.TenantID)
	if err != nil {
		c.Error(err)
		return
	}

	c.JSON(http.StatusOK, dto.GetUserPermissionsResponse{
		Message:     "User permissions retrieved successfully",
		UserID:      req.UserID,
		TenantID:    req.TenantID,
		Permissions: permissions,
	})
}
