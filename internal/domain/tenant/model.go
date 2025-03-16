package tenant

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// Tenant represents an organization or group within the system.
type TenantBillingInfo struct {
	Address   Address `json:"address"`
	Email     string  `json:"email"`
	Website   string  `json:"website"`
	HelpEmail string  `json:"help_email"`
}

type Address struct {
	Street     string `json:"street"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

type Tenant struct {
	ID                string            `json:"id"`
	Name              string            `json:"name"`
	Status            types.Status      `json:"status"`
	CreatedAt         time.Time         `json:"created_at"`
	UpdatedAt         time.Time         `json:"updated_at"`
	TenantBillingInfo TenantBillingInfo `json:"tenant_billing_info"`
}

// FromEnt converts an ent Tenant to a domain Tenant
func FromEnt(e *ent.Tenant) *Tenant {
	if e == nil {
		return nil
	}

	// parse billing info from e.BillingInfo map
	billingInfo := TenantBillingInfo{}
	if e.BillingInfo != nil {
		address := e.BillingInfo["address"].(map[string]interface{})

		street := address["address_line_1"].(string)
		if v, ok := address["address_line_2"]; ok {
			street += "\n" + v.(string)
		}

		city := address["city"].(string)
		state := address["state"].(string)
		postalCode := address["postal_code"].(string)

		email := ""
		if v, ok := e.BillingInfo["email"]; ok {
			email = v.(string)
		}

		website := ""
		if v, ok := e.BillingInfo["website"]; ok {
			website = v.(string)
		}

		helpEmail := ""
		if v, ok := e.BillingInfo["help_email"]; ok {
			helpEmail = v.(string)
		} else {
			helpEmail = email
		}

		billingInfo = TenantBillingInfo{
			Address: Address{
				Street:     street,
				City:       city,
				State:      state,
				PostalCode: postalCode,
			},
			Email:     email,
			Website:   website,
			HelpEmail: helpEmail,
		}
	}
	return &Tenant{
		ID:                e.ID,
		Name:              e.Name,
		Status:            types.Status(e.Status),
		TenantBillingInfo: billingInfo,
		CreatedAt:         e.CreatedAt,
		UpdatedAt:         e.UpdatedAt,
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
