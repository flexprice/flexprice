package dto

import (
	"encoding/json"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

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
	return validator.ValidateRequest(r)
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
