package tenant

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/types"
)

type TenantBillingDetails struct {
	Email     string        `json:"email,omitempty"`
	HelpEmail string        `json:"help_email,omitempty"`
	Phone     string        `json:"phone,omitempty"`
	Address   TenantAddress `json:"address,omitempty"`
}

// TenantAddress represents a physical address in the tenant billing details
type TenantAddress struct {
	Line1      string `json:"address_line1,omitempty"`
	Line2      string `json:"address_line2,omitempty"`
	City       string `json:"address_city,omitempty"`
	State      string `json:"address_state,omitempty"`
	PostalCode string `json:"address_postal_code,omitempty"`
	Country    string `json:"address_country,omitempty"`
}

// Tenant represents an organization or group within the system.
type Tenant struct {
	ID             string               `json:"id"`
	Name           string               `json:"name"`
	Status         types.Status         `json:"status"`
	BillingDetails TenantBillingDetails `json:"billing_details"`
	Metadata       types.Metadata       `json:"metadata"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

// FromEnt converts an ent Tenant to a domain Tenant
func FromEnt(e *ent.Tenant) *Tenant {
	if e == nil {
		return nil
	}

	return &Tenant{
		ID:             e.ID,
		Name:           e.Name,
		Status:         types.Status(e.Status),
		Metadata:       e.Metadata,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      e.UpdatedAt,
		BillingDetails: FromEntBillingDetails(e.BillingDetails),
	}
}

// FromEntList converts a list of ent Tenants to domain Tenants
func FromEntList(tenants []*ent.Tenant) []*Tenant {
	if tenants == nil {
		return nil
	}

	result := make([]*Tenant, len(tenants))
	for i, t := range tenants {
		result[i] = FromEnt(t)
	}

	return result
}

func FromEntBillingDetails(e schema.TenantBillingDetails) TenantBillingDetails {
	return TenantBillingDetails{
		Email:     e.Email,
		HelpEmail: e.HelpEmail,
		Phone:     e.Phone,
		Address:   FromEntTenantAddress(e.Address),
	}
}

func FromEntTenantAddress(e schema.TenantAddress) TenantAddress {
	return TenantAddress{
		Line1:      e.Line1,
		Line2:      e.Line2,
		City:       e.City,
		State:      e.State,
		PostalCode: e.PostalCode,
		Country:    e.Country,
	}
}

func (t TenantBillingDetails) ToSchema() schema.TenantBillingDetails {
	return schema.TenantBillingDetails{
		Email:     t.Email,
		HelpEmail: t.HelpEmail,
		Phone:     t.Phone,
		Address:   t.Address.ToSchema(),
	}
}

func (t TenantAddress) ToSchema() schema.TenantAddress {
	return schema.TenantAddress{
		Line1:      t.Line1,
		Line2:      t.Line2,
		City:       t.City,
		State:      t.State,
		PostalCode: t.PostalCode,
		Country:    t.Country,
	}
}
