package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type TenantBillingDetails struct {
	Email     string  `json:"email,omitempty"`
	HelpEmail string  `json:"help_email,omitempty"`
	Phone     string  `json:"phone,omitempty"`
	Address   Address `json:"address,omitempty"`
}

func NewTenantBillingDetails(b tenant.BillingDetails) TenantBillingDetails {
	return TenantBillingDetails{
		Email:     b.Email,
		HelpEmail: b.HelpEmail,
		Phone:     b.Phone,
		Address: Address{
			Line1:      b.Address.Line1,
			Line2:      b.Address.Line2,
			City:       b.Address.City,
			State:      b.Address.State,
			PostalCode: b.Address.PostalCode,
			Country:    b.Address.Country,
		},
	}
}
func (r *TenantBillingDetails) ToDomain() tenant.BillingDetails {
	return tenant.BillingDetails{
		Email:     r.Email,
		HelpEmail: r.HelpEmail,
		Phone:     r.Phone,
		Address: tenant.Address{
			Line1:      r.Address.Line1,
			Line2:      r.Address.Line2,
			City:       r.Address.City,
			State:      r.Address.State,
			PostalCode: r.Address.PostalCode,
			Country:    r.Address.Country,
		},
	}
}

type CreateTenantRequest struct {
	Name           string               `json:"name" validate:"required"`
	BillingDetails TenantBillingDetails `json:"billing_details,omitempty"`
}

type TenantResponse struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	BillingDetails TenantBillingDetails `json:"billing_details,omitempty"`
	Status         string               `json:"status"`
	CreatedAt      string               `json:"created_at"`
	UpdatedAt      string               `json:"updated_at"`
}

type AssignTenantRequest struct {
	UserID   string `json:"user_id" validate:"required,uuid"`
	TenantID string `json:"tenant_id" validate:"required,uuid"`
}

func (r *CreateTenantRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *CreateTenantRequest) ToTenant(ctx context.Context) *tenant.Tenant {
	return &tenant.Tenant{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TENANT),
		Name:           r.Name,
		Status:         types.StatusPublished,
		BillingDetails: r.BillingDetails.ToDomain(),
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

}

func (r *AssignTenantRequest) Validate(ctx context.Context) error {
	return validator.ValidateRequest(r)
}

func NewTenantResponse(t *tenant.Tenant) *TenantResponse {
	return &TenantResponse{
		ID:             t.ID,
		Name:           t.Name,
		Status:         string(t.Status),
		CreatedAt:      t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      t.UpdatedAt.Format(time.RFC3339),
		BillingDetails: NewTenantBillingDetails(t.BillingDetails),
	}
}

type UpdateTenantRequest struct {
	BillingDetails TenantBillingDetails `json:"billing_details,omitempty"`
}

func (r *UpdateTenantRequest) Validate() error {
	return validator.ValidateRequest(r)
}
