package billingentity

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/types"
)

// BillingEntity is a legal entity of a tenant that bills customers in one environment.
type BillingEntity struct {
	ID                  string                `json:"id"`
	Name                string                `json:"name"`
	LegalName           string                `json:"legal_name"`
	IsDefault           bool                  `json:"is_default"`
	InvoiceNumberPrefix string                `json:"invoice_number_prefix"`
	SellerDetails       types.SellerDetails   `json:"seller_details"`
	TaxIdentifiers      []types.TaxIdentifier `json:"tax_identifiers"`
	Metadata            map[string]string     `json:"metadata,omitempty"`
	EnvironmentID       string                `json:"environment_id"`
	types.BaseModel
}

// SellerSnapshot freezes the entity as an invoice's seller.
func (e *BillingEntity) SellerSnapshot() *types.SellerSnapshot {
	if e == nil {
		return nil
	}
	return &types.SellerSnapshot{
		BillingEntityID: e.ID,
		LegalName:       e.LegalName,
		SellerDetails:   e.SellerDetails,
		TaxIdentifiers:  append([]types.TaxIdentifier(nil), e.TaxIdentifiers...),
	}
}

// SellerDetailsFromTenant copies tenant billing details into the seller details shape.
func SellerDetailsFromTenant(d tenant.TenantBillingDetails) types.SellerDetails {
	return types.SellerDetails{
		Email:     d.Email,
		HelpEmail: d.HelpEmail,
		Phone:     d.Phone,
		Address: types.SellerAddress{
			Line1:      d.Address.Line1,
			Line2:      d.Address.Line2,
			City:       d.Address.City,
			State:      d.Address.State,
			PostalCode: d.Address.PostalCode,
			Country:    d.Address.Country,
		},
	}
}

// TenantBillingDetailsFromSeller maps seller details back to the tenant billing details shape.
func TenantBillingDetailsFromSeller(d types.SellerDetails) tenant.TenantBillingDetails {
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

// FromEnt converts an Ent BillingEntity to a domain BillingEntity
func FromEnt(e *ent.BillingEntity) *BillingEntity {
	if e == nil {
		return nil
	}
	return &BillingEntity{
		ID:                  e.ID,
		Name:                e.Name,
		LegalName:           e.LegalName,
		IsDefault:           e.IsDefault,
		InvoiceNumberPrefix: e.InvoiceNumberPrefix,
		SellerDetails:       e.SellerDetails,
		TaxIdentifiers:      e.TaxIdentifiers,
		Metadata:            e.Metadata,
		EnvironmentID:       e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
}

// FromEntList converts a list of Ent BillingEntities to domain BillingEntities
func FromEntList(list []*ent.BillingEntity) []*BillingEntity {
	if list == nil {
		return nil
	}
	entities := make([]*BillingEntity, len(list))
	for i, item := range list {
		entities[i] = FromEnt(item)
	}
	return entities
}
