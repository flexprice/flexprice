package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type CreateTenantRequest struct {
	Name string `json:"name" validate:"required"`
}

type TenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type AssignTenantRequest struct {
	UserID   string `json:"user_id" validate:"required,uuid"`
	TenantID string `json:"tenant_id" validate:"required,uuid"`
}

func (r *CreateTenantRequest) Validate() error {
	return validator.New().Struct(r)
}

func (r *CreateTenantRequest) ToTenant(ctx context.Context) *tenant.Tenant {
	return &tenant.Tenant{
		ID:        uuid.New().String(),
		Name:      r.Name,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
}

func (r *AssignTenantRequest) Validate(ctx context.Context) error {
	err := validator.New().Struct(r)
	if err != nil {
		return err
	}
	return nil
}

func NewTenantResponse(t *tenant.Tenant) *TenantResponse {
	return &TenantResponse{
		ID:        t.ID,
		Name:      t.Name,
		CreatedAt: t.CreatedAt.Format(time.RFC3339),
		UpdatedAt: t.UpdatedAt.Format(time.RFC3339),
	}
}
