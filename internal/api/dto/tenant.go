package dto

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
)

type TenantBillingDetails struct {
	Email     string  `json:"email,omitempty"`
	HelpEmail string  `json:"help_email,omitempty"`
	Phone     string  `json:"phone,omitempty"`
	Address   Address `json:"address,omitempty"`
}

func NewTenantBillingDetails(b tenant.TenantBillingDetails) TenantBillingDetails {
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
func (r *TenantBillingDetails) ToDomain() tenant.TenantBillingDetails {
	return tenant.TenantBillingDetails{
		Email:     r.Email,
		HelpEmail: r.HelpEmail,
		Phone:     r.Phone,
		Address: tenant.TenantAddress{
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
	Name           string                `json:"name" validate:"required"`
	BillingDetails *TenantBillingDetails `json:"billing_details,omitempty"`
	Metadata       map[string]string     `json:"metadata,omitempty"`
	ID             string                `json:"-"`
}

type TenantResponse struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	BillingDetails *TenantBillingDetails `json:"billing_details,omitempty"`
	Status         string                `json:"status"`
	CreatedAt      string                `json:"created_at"`
	UpdatedAt      string                `json:"updated_at"`
	Metadata       *types.Metadata       `json:"metadata,omitempty"`
}

type AssignTenantRequest struct {
	UserID   string `json:"user_id" validate:"required,uuid"`
	TenantID string `json:"tenant_id" validate:"required,uuid"`
}

func (r *CreateTenantRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *CreateTenantRequest) ToTenant(ctx context.Context) *tenant.Tenant {
	var billingDetails tenant.TenantBillingDetails
	if r.BillingDetails != nil {
		billingDetails = r.BillingDetails.ToDomain()
	}

	if r.ID == "" {
		r.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TENANT)
	}

	return &tenant.Tenant{
		ID:             r.ID,
		Name:           r.Name,
		Status:         types.StatusPublished,
		InternalStatus: types.TenantInternalStatusTrialing,
		BillingDetails: billingDetails,
		Metadata:       r.Metadata,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

}

func (r *AssignTenantRequest) Validate(ctx context.Context) error {
	return validator.ValidateRequest(r)
}

func NewTenantResponse(t *tenant.Tenant) *TenantResponse {
	var billingDetails TenantBillingDetails
	// how can we improve this?
	if t.BillingDetails != (tenant.TenantBillingDetails{}) {
		billingDetails = NewTenantBillingDetails(t.BillingDetails)
	}
	return &TenantResponse{
		ID:             t.ID,
		Name:           t.Name,
		Status:         string(t.Status),
		CreatedAt:      t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      t.UpdatedAt.Format(time.RFC3339),
		BillingDetails: &billingDetails,
		Metadata:       &t.Metadata,
	}
}

type UpdateTenantRequest struct {
	Name           *string                     `json:"name,omitempty"`
	BillingDetails *UpdateTenantBillingDetails `json:"billing_details,omitempty"`
	Metadata       *types.Metadata             `json:"metadata,omitempty"`
}

// UpdateTenantBillingDetails is a partial update: an omitted field keeps the stored value, "" clears it.
type UpdateTenantBillingDetails struct {
	Email     *string              `json:"email,omitempty"`
	HelpEmail *string              `json:"help_email,omitempty"`
	Phone     *string              `json:"phone,omitempty"`
	Address   *UpdateTenantAddress `json:"address,omitempty"`
}

// UpdateTenantAddress is a partial address update with the same semantics as UpdateTenantBillingDetails.
type UpdateTenantAddress struct {
	Line1      *string `json:"address_line1,omitempty"`
	Line2      *string `json:"address_line2,omitempty"`
	City       *string `json:"address_city,omitempty"`
	State      *string `json:"address_state,omitempty"`
	PostalCode *string `json:"address_postal_code,omitempty"`
	Country    *string `json:"address_country,omitempty"`
}

func (r *UpdateTenantRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	if r.BillingDetails == nil || r.BillingDetails.Address == nil {
		return nil
	}
	// Validate the provided values with the Address rules, where "" is allowed.
	a := r.BillingDetails.Address
	return validator.ValidateRequest(Address{
		Line1:      lo.FromPtr(a.Line1),
		Line2:      lo.FromPtr(a.Line2),
		City:       lo.FromPtr(a.City),
		State:      lo.FromPtr(a.State),
		PostalCode: lo.FromPtr(a.PostalCode),
		Country:    lo.FromPtr(a.Country),
	})
}

// MergeInto returns existing with every provided field replaced.
func (r *UpdateTenantBillingDetails) MergeInto(existing tenant.TenantBillingDetails) tenant.TenantBillingDetails {
	if r == nil {
		return existing
	}
	merged := existing
	setIfProvided(&merged.Email, r.Email)
	setIfProvided(&merged.HelpEmail, r.HelpEmail)
	setIfProvided(&merged.Phone, r.Phone)
	if a := r.Address; a != nil {
		setIfProvided(&merged.Address.Line1, a.Line1)
		setIfProvided(&merged.Address.Line2, a.Line2)
		setIfProvided(&merged.Address.City, a.City)
		setIfProvided(&merged.Address.State, a.State)
		setIfProvided(&merged.Address.PostalCode, a.PostalCode)
		setIfProvided(&merged.Address.Country, a.Country)
	}
	return merged
}

func setIfProvided(dst *string, src *string) {
	if src != nil {
		*dst = *src
	}
}

type TenantBillingUsage struct {
	Usage         *CustomerUsageSummaryResponse `json:"usage"`
	Subscriptions []*SubscriptionResponse       `json:"subscriptions"`
}
