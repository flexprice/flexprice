package dto

import (
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateUserRequest represents the request to create a new user (service accounts only)
type CreateUserRequest struct {
	Type  types.UserType `json:"type" binding:"required" validate:"required"`              // Must be "service_account"
	Roles []string       `json:"roles" binding:"required,min=1" validate:"required,min=1"` // Roles are required
}

func (r *CreateUserRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Validate the user type enum
	if err := r.Type.Validate(); err != nil {
		return err
	}

	// Only service accounts can be created via API
	if r.Type != types.UserTypeServiceAccount {
		return ierr.NewError("only service accounts can be created via this endpoint").
			WithHint("Regular user accounts cannot be created via API. Use type='service_account'").
			Mark(ierr.ErrValidation)
	}

	// Service accounts MUST have roles (already validated by binding tag, but double-check)
	if len(r.Roles) == 0 {
		return ierr.NewError("service accounts must have at least one role").
			WithHint("Service accounts require role assignment").
			Mark(ierr.ErrValidation)
	}

	return nil
}

type UserResponse struct {
	ID     string          `json:"id"`
	Email  string          `json:"email,omitempty"` // Empty for service accounts
	Type   types.UserType  `json:"type"`
	Roles  []string        `json:"roles,omitempty"`
	Tenant *TenantResponse `json:"tenant"`
}

func NewUserResponse(u *user.User, tenant *tenant.Tenant) *UserResponse {
	return &UserResponse{
		ID:     u.ID,
		Email:  u.Email,
		Type:   u.Type,
		Roles:  u.Roles,
		Tenant: NewTenantResponse(tenant),
	}
}

// ListUsersResponse is the response type for listing users with pagination
type ListUsersResponse = types.ListResponse[*UserResponse]
