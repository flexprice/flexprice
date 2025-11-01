package dto

import (
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateUserRequest represents the request to create a new user
type CreateUserRequest struct {
	Email    string   `json:"email" binding:"omitempty,email" validate:"omitempty,email"` // Required only for type=user
	Type     string   `json:"type" binding:"omitempty" validate:"omitempty"`              // Default: "user"
	Roles    []string `json:"roles,omitempty"`
	TenantID string   `json:"tenant_id,omitempty"` // Optional - will use from context if not provided
}

func (r *CreateUserRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Default type to "user" if not provided
	if r.Type == "" {
		r.Type = string(user.UserTypeUser)
	}

	// Validate user type using enum
	userType := user.UserType(r.Type)
	if err := userType.Validate(); err != nil {
		return err
	}

	// Service accounts MUST have roles
	if userType == user.UserTypeServiceAccount && len(r.Roles) == 0 {
		return ierr.NewError("service accounts must have at least one role").
			WithHint("Service accounts require role assignment").
			Mark(ierr.ErrValidation)
	}

	// Regular users MUST have email
	if userType == user.UserTypeUser && r.Email == "" {
		return ierr.NewError("email is required for user type").
			WithHint("Regular users must have an email address").
			Mark(ierr.ErrValidation)
	}

	// Service accounts CANNOT have email
	if userType == user.UserTypeServiceAccount && r.Email != "" {
		return ierr.NewError("service accounts cannot have email").
			WithHint("Service accounts do not use email addresses").
			Mark(ierr.ErrValidation)
	}

	return nil
}

type UserResponse struct {
	ID     string          `json:"id"`
	Email  string          `json:"email,omitempty"` // Empty for service accounts
	Type   string          `json:"type"`
	Roles  []string        `json:"roles,omitempty"`
	Tenant *TenantResponse `json:"tenant"`
}

func NewUserResponse(user *user.User, tenant *tenant.Tenant) *UserResponse {
	return &UserResponse{
		ID:     user.ID,
		Email:  user.Email,
		Type:   user.Type,
		Roles:  user.Roles,
		Tenant: NewTenantResponse(tenant),
	}
}
