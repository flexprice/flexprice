package billingentity

import (
	"errors"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
)

// ErrNotEnabled marks an entity endpoint, field or filter used in an environment that is not enabled.
var ErrNotEnabled = errors.New("billing entities not enabled")

// BillingEntity is a legal entity of a tenant that bills customers in one environment.
type BillingEntity struct {
	ID                  string               `json:"id"`
	Name                string               `json:"name"`
	IsDefault           bool                 `json:"is_default"`
	InvoiceNumberPrefix string               `json:"invoice_number_prefix"`
	BillingDetails      types.BillingDetails `json:"billing_details"`
	Metadata            map[string]string    `json:"metadata,omitempty"`
	EnvironmentID       string               `json:"environment_id"`
	types.BaseModel
}

// AsInvoiceBillingEntity freezes the entity as an invoice's billing entity.
func (e *BillingEntity) AsInvoiceBillingEntity() *types.InvoiceBillingEntity {
	if e == nil {
		return nil
	}
	return &types.InvoiceBillingEntity{
		BillingEntityID: e.ID,
		Name:            e.Name,
		BillingDetails:  e.BillingDetails,
	}
}

// BillingDetailsFromTenant copies tenant billing details into the entity billing details shape.
func BillingDetailsFromTenant(d tenant.TenantBillingDetails) types.BillingDetails {
	return types.BillingDetails{
		Email:     d.Email,
		HelpEmail: d.HelpEmail,
		Phone:     d.Phone,
		Address: types.BillingAddress{
			Line1:      d.Address.Line1,
			Line2:      d.Address.Line2,
			City:       d.Address.City,
			State:      d.Address.State,
			PostalCode: d.Address.PostalCode,
			Country:    d.Address.Country,
		},
	}
}

// ToTenantBillingDetails maps entity billing details back to the tenant billing details shape.
func ToTenantBillingDetails(d types.BillingDetails) tenant.TenantBillingDetails {
	return tenant.TenantBillingDetails{
		Email:     d.Email,
		HelpEmail: d.HelpEmail,
		Phone:     d.Phone,
		Address: tenant.TenantAddress{
			Line1:      d.Address.Line1,
			Line2:      d.Address.Line2,
			City:       d.Address.City,
			State:      d.Address.State,
			PostalCode: d.Address.PostalCode,
			Country:    d.Address.Country,
		},
	}
}
