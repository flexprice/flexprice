package dto

// RoleAssignmentRequest represents a request to assign a role to a user
type RoleAssignmentRequest struct {
	UserID string `json:"user_id" binding:"required"`
	Role   string `json:"role" binding:"required"`
}

// RoleAssignmentResponse represents a response for role assignment
type RoleAssignmentResponse struct {
	UserID   string `json:"user_id"`
	Role     string `json:"role"`
	TenantID string `json:"tenant_id"`
	Message  string `json:"message"`
}

// UserRolesResponse represents a response containing user roles
type UserRolesResponse struct {
	UserID string   `json:"user_id"`
	Roles  []string `json:"roles"`
}

// PolicyRequest represents a request to create or update a policy
type PolicyRequest struct {
	Role     string `json:"role" binding:"required"`
	Resource string `json:"resource" binding:"required"`
	Action   string `json:"action" binding:"required"`
	Effect   string `json:"effect" binding:"required"`
}

// PolicyResponse represents a response for policy operations
type PolicyResponse struct {
	ID       string `json:"id"`
	Role     string `json:"role"`
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Effect   string `json:"effect"`
	TenantID string `json:"tenant_id"`
}

// AuthorizationCheckRequest represents a request to check authorization
type AuthorizationCheckRequest struct {
	Resource string `json:"resource" binding:"required"`
	Action   string `json:"action" binding:"required"`
}

// AuthorizationCheckResponse represents a response for authorization check
type AuthorizationCheckResponse struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason,omitempty"`
}

// AuditLogEntry represents an authorization audit log entry
type AuditLogEntry struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	TenantID  string `json:"tenant_id"`
	Resource  string `json:"resource"`
	Action    string `json:"action"`
	Allowed   bool   `json:"allowed"`
	Reason    string `json:"reason,omitempty"`
	IPAddress string `json:"ip_address,omitempty"`
	UserAgent string `json:"user_agent,omitempty"`
	CreatedAt string `json:"created_at"`
}

// CreateRoleRequest represents a request to create a new role
type CreateRoleRequest struct {
	Name        string   `json:"name" binding:"required"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"` // Array of "resource:action" strings
}

// RoleResponse represents a role with its permissions
type RoleResponse struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
	TenantID    string   `json:"tenant_id"`
	CreatedAt   string   `json:"created_at"`
	UpdatedAt   string   `json:"updated_at"`
}

// UpdateRoleRequest represents a request to update a role
type UpdateRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Permissions []string `json:"permissions"`
}

// ListRolesResponse represents a response containing all roles
type ListRolesResponse struct {
	Roles []RoleResponse `json:"roles"`
	Total int            `json:"total"`
}
