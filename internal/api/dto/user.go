package dto

import (
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateUserRequest represents the request to create a new user (service account or user)
type CreateUserRequest struct {
	Type  types.UserType `json:"type" binding:"required" validate:"required"` // "user" or "service_account"
	Name  string         `json:"name,omitempty" validate:"omitempty"`         // Display name; optional for service accounts
	Roles []string       `json:"roles,omitempty" validate:"omitempty"`        // Required when type is "service_account"
	Email string         `json:"email,omitempty" validate:"omitempty,email"`  // Required when type is "user"
}

func (r *CreateUserRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.Type.Validate(); err != nil {
		return err
	}

	switch r.Type {
	case types.UserTypeUser:
		if r.Email == "" {
			return ierr.NewError("email is required for user accounts").
				WithHint("Provide a valid email when creating a user (type='user')").
				Mark(ierr.ErrValidation)
		}
		// No roles required for user type
	case types.UserTypeServiceAccount:
		if len(r.Roles) == 0 {
			return ierr.NewError("service accounts must have at least one role").
				WithHint("Service accounts require role assignment").
				Mark(ierr.ErrValidation)
		}
		if r.Email != "" {
			return ierr.NewError("service accounts must not have an email").
				WithHint("Omit email when creating a service account").
				Mark(ierr.ErrValidation)
		}
	default:
		return ierr.NewError("invalid user type").
			WithHint("Type must be 'user' or 'service_account'").
			Mark(ierr.ErrValidation)
	}

	return nil
}

type UserResponse struct {
	ID       string            `json:"id"`
	Name     string            `json:"name,omitempty"`
	Email    string            `json:"email,omitempty"` // Empty for service accounts
	Type     types.UserType    `json:"type"`
	Roles    []string          `json:"roles,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	Tenant   *TenantResponse   `json:"tenant"`
}

// CreateUserResponse is the response for POST /users: same shape for both types; password only when type=user.
type CreateUserResponse struct {
	*UserResponse
	Password string `json:"password,omitempty"`
}

func NewUserResponse(u *user.User, tenant *tenant.Tenant) *UserResponse {
	return &UserResponse{
		ID:       u.ID,
		Name:     u.Name,
		Email:    u.Email,
		Type:     u.Type,
		Roles:    u.Roles,
		Metadata: u.Metadata,
		Tenant:   NewTenantResponse(tenant),
	}
}

type UpdateUserRequest struct {
	Name     string            `json:"name,omitempty" validate:"omitempty"`
	Metadata map[string]string `json:"metadata,omitempty" validate:"omitempty"`
}

func (r *UpdateUserRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	return nil
}

type UpdateUserResponse struct {
	*UserResponse
}

// UpdateServiceAccountRequest is the request body for PUT /users/:id (service accounts only)
type UpdateServiceAccountRequest struct {
	Name string `json:"name" binding:"required"`
}

func (r *UpdateServiceAccountRequest) Validate() error {
	if r.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Service account name cannot be empty").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type UpdateServiceAccountResponse struct {
	*UserResponse
}

// UpdateUserRolesRequest is the request body for PUT /users/:id/roles.
// Only supported for type=user accounts; service account roles are fixed at creation.
type UpdateUserRolesRequest struct {
	Roles []string `json:"roles" validate:"omitempty"`
}

func (r *UpdateUserRolesRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if len(r.Roles) == 0 {
		return ierr.NewError("roles is required").
			WithHint("Provide at least one role").
			Mark(ierr.ErrValidation)
	}
	for _, role := range r.Roles {
		if role == "" {
			return ierr.NewError("roles cannot contain empty values").
				WithHint("Remove empty entries from roles").
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// UpdateUserRolesResponse is the response for PUT /users/:id/roles.
type UpdateUserRolesResponse struct {
	*UserResponse
}

// ActiveAPIKey identifies a single active API key in the active-keys error
// payload returned when a role update is blocked.
type ActiveAPIKey struct {
	ID      string `json:"id"`
	KeyName string `json:"key_name"`
}

// ActiveEnvironmentAPIKeys groups a user's active API keys by the environment
// they belong to — keyed by environment ID in the parent map (stable and
// unique, unlike environment name), with the name carried here for display.
type ActiveEnvironmentAPIKeys struct {
	EnvName string         `json:"env_name"`
	APIKeys []ActiveAPIKey `json:"api_keys"`
}

// Token is the hex-encoded HMAC-SHA256 of the user's email, verified by Pylon via email_hash.
type SupportChatTokenResponse struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// ListUsersResponse is the response type for listing users with pagination
type ListUsersResponse = types.ListResponse[*UserResponse] // @name ListUsersResponse
