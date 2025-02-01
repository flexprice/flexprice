package dto

import (
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/domain/user"
)

type UserResponse struct {
	ID     string          `json:"id"`
	Email  string          `json:"email"`
	Tenant *TenantResponse `json:"tenant"`
}

func NewUserResponse(user *user.User, tenant *tenant.Tenant) *UserResponse {
	return &UserResponse{
		ID:    user.ID,
		Email: user.Email,
		Tenant: &TenantResponse{
			ID:   tenant.ID,
			Name: tenant.Name,
		},
	}
}
