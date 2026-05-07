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
	ID     string          `json:"id"`
	Email  string          `json:"email,omitempty"` // Empty for service accounts
	Type   types.UserType  `json:"type"`
	Roles  []string        `json:"roles,omitempty"`
	Tenant *TenantResponse `json:"tenant"`
}

// CreateUserResponse is the response for POST /users: same shape for both types; password only when type=user.
type CreateUserResponse struct {
	*UserResponse
	Password string `json:"password,omitempty"`
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
type ListUsersResponse = types.ListResponse[*UserResponse] // @name ListUsersResponse
