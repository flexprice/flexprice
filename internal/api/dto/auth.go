package dto

import (
	"github.com/flexprice/flexprice/internal/validator"
)

type SignUpRequest struct {
	Email      string `json:"email" binding:"required,email" validate:"email"`
	Password   string `json:"password" binding:"omitempty,min=8" validate:"omitempty,min=8"`
	TenantName string `json:"tenant_name" binding:"omitempty" validate:"omitempty"`
	Token      string `json:"token" binding:"omitempty" validate:"omitempty"`
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
