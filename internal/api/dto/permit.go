package dto

// CreatePermitTenantRequest represents a request to create a new tenant in Permit.io
type CreatePermitTenantRequest struct {
	TenantID string `json:"tenant_id" binding:"required"`
	Name     string `json:"name" binding:"required"`
}

// CreatePermitTenantResponse represents a response from creating a tenant in Permit.io
type CreatePermitTenantResponse struct {
	Message  string `json:"message"`
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
}

// SyncUserRequest represents a request to sync a user to Permit.io
type SyncUserRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	TenantID string `json:"tenant_id" binding:"required"`
}

// SyncUserResponse represents a response from syncing a user
type SyncUserResponse struct {
	Message string `json:"message"`
	UserID  string `json:"user_id"`
}

// AssignRoleRequest represents a request to assign a role to a user
type AssignRoleRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	RoleKey  string `json:"role_key" binding:"required"`
	TenantID string `json:"tenant_id" binding:"required"`
}

// AssignRoleResponse represents a response from assigning a role
type AssignRoleResponse struct {
	Message string `json:"message"`
	UserID  string `json:"user_id"`
	RoleKey string `json:"role_key"`
}

// CreateRoleRequest represents a request to create a new role
type CreateRoleRequest struct {
	RoleKey     string   `json:"role_key" binding:"required"`
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	TenantID    string   `json:"tenant_id" binding:"required"`
	Permissions []string `json:"permissions"`
}

// CreateRoleResponse represents a response from creating a role
type CreateRoleResponse struct {
	Message  string `json:"message"`
	RoleKey  string `json:"role_key"`
	Name     string `json:"name"`
	TenantID string `json:"tenant_id"`
}

// CheckPermissionRequest represents a request to check user permissions
type CheckPermissionRequest struct {
	UserID   string `json:"user_id" binding:"required"`
	Action   string `json:"action" binding:"required"`
	Resource string `json:"resource" binding:"required"`
	TenantID string `json:"tenant_id" binding:"required"`
}

// CheckPermissionResponse represents a response from checking permissions
type CheckPermissionResponse struct {
	Allowed  bool   `json:"allowed"`
	UserID   string `json:"user_id"`
	Action   string `json:"action"`
	Resource string `json:"resource"`
	TenantID string `json:"tenant_id"`
}

// GetTenantRolesResponse represents a response for getting tenant roles
type GetTenantRolesResponse struct {
	Message  string   `json:"message"`
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles"`
}

// GetTenantResourcesResponse represents a response for getting tenant resources
type GetTenantResourcesResponse struct {
	Message   string   `json:"message"`
	TenantID  string   `json:"tenant_id"`
	Resources []string `json:"resources"`
}

// DebugUserResponse represents a response for debugging user permissions
type DebugUserResponse struct {
	UserID          string          `json:"user_id"`
	TenantID        string          `json:"tenant_id"`
	UserRoles       []string        `json:"user_roles"`
	TenantRoles     []string        `json:"tenant_roles"`
	TenantResources []string        `json:"tenant_resources"`
	Permissions     map[string]bool `json:"permissions"`
}

// ConfigureRoleResponse represents a response for role configuration
type ConfigureRoleResponse struct {
	Message           string   `json:"message"`
	TenantID          string   `json:"tenant_id"`
	Resources         []string `json:"resources"`
	Actions           []string `json:"actions"`
	Instructions      []string `json:"instructions"`
	PermissionsNeeded []string `json:"permissions_needed"`
}

// SyncResourceResponse represents a response for syncing a resource
type SyncResourceResponse struct {
	Message    string `json:"message"`
	ResourceID string `json:"resource_id"`
	TenantID   string `json:"tenant_id"`
}
