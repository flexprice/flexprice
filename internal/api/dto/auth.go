package dto

import (
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// maxPasswordBytes is the most bcrypt will hash; longer passwords are refused rather than truncated.
const maxPasswordBytes = 72

type SignUpRequest struct {
	Email      string            `json:"email" binding:"required,email" validate:"email"`
	Password   string            `json:"password" binding:"omitempty,min=8" validate:"omitempty,min=8"`
	TenantName string            `json:"tenant_name" binding:"omitempty" validate:"omitempty"`
	Token      string            `json:"token" binding:"omitempty" validate:"omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty" binding:"omitempty" validate:"omitempty"`
}

type LoginRequest struct {
	Email    string `json:"email" binding:"required,email" validate:"email"`
	Password string `json:"password" binding:"required" validate:"min=8"`
	Token    string `json:"token" binding:"omitempty" validate:"omitempty"`
}

type AuthResponse struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	TenantID string `json:"tenant_id"`
}

func (r *SignUpRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	return validatePasswordLength(r.Password)
}

func (r *LoginRequest) Validate() error {
	return validator.ValidateRequest(r)
}

// UnmarshalJSON normalises email before binding validation runs.
func (r *SignUpRequest) UnmarshalJSON(data []byte) error {
	type plain SignUpRequest
	if err := json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.Email = types.NormalizeEmail(r.Email)
	return nil
}

// UnmarshalJSON normalises email before binding validation runs.
func (r *LoginRequest) UnmarshalJSON(data []byte) error {
	type plain LoginRequest
	if err := json.Unmarshal(data, (*plain)(r)); err != nil {
		return err
	}
	r.Email = types.NormalizeEmail(r.Email)
	return nil
}

// CheckPasswordLinkRequest asks what a one-time password link is for.
type CheckPasswordLinkRequest struct {
	Token string `json:"token" binding:"required"`
}

// PasswordLinkDetailsResponse tells the set-password page what to show.
type PasswordLinkDetailsResponse struct {
	Purpose string `json:"purpose"`
	Email   string `json:"email"`
}

// SetPasswordRequest sets a password through a one-time link.
type SetPasswordRequest struct {
	Token    string `json:"token" binding:"required" validate:"required"`
	Password string `json:"password" binding:"required,min=8" validate:"required,min=8"`
}

func (r *SetPasswordRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	return validatePasswordLength(r.Password)
}

func validatePasswordLength(password string) error {
	if len(password) > maxPasswordBytes {
		return ierr.NewError("password longer than bcrypt accepts").
			WithHint("Password must be at most 72 characters").
			Mark(ierr.ErrValidation)
	}
	return nil
}
