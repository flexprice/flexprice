package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateBillingEntityRequest creates a non-default billing entity.
type CreateBillingEntityRequest struct {
	Name                string                `json:"name" validate:"required,max=255"`
	LegalName           string                `json:"legal_name" validate:"required,max=255"`
	InvoiceNumberPrefix string                `json:"invoice_number_prefix" validate:"required,min=1,max=20"`
	SellerDetails       types.SellerDetails   `json:"seller_details"`
	TaxIdentifiers      []types.TaxIdentifier `json:"tax_identifiers,omitempty"`
	Metadata            map[string]string     `json:"metadata,omitempty"`
}

func (r *CreateBillingEntityRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *CreateBillingEntityRequest) ToBillingEntity(ctx context.Context) *billingentity.BillingEntity {
	return &billingentity.BillingEntity{
		ID:                  types.GenerateUUIDWithPrefix(types.UUID_PREFIX_BILLING_ENTITY),
		Name:                r.Name,
		LegalName:           r.LegalName,
		InvoiceNumberPrefix: r.InvoiceNumberPrefix,
		SellerDetails:       r.SellerDetails,
		TaxIdentifiers:      r.TaxIdentifiers,
		Metadata:            r.Metadata,
		EnvironmentID:       types.GetEnvironmentID(ctx),
		BaseModel:           types.GetDefaultBaseModel(ctx),
	}
}

// UpdateBillingEntityRequest edits an entity; only the fields sent change.
type UpdateBillingEntityRequest struct {
	Name                *string                `json:"name,omitempty" validate:"omitempty,min=1,max=255"`
	LegalName           *string                `json:"legal_name,omitempty" validate:"omitempty,min=1,max=255"`
	InvoiceNumberPrefix *string                `json:"invoice_number_prefix,omitempty" validate:"omitempty,min=1,max=20"`
	SellerDetails       *types.SellerDetails   `json:"seller_details,omitempty"`
	TaxIdentifiers      *[]types.TaxIdentifier `json:"tax_identifiers,omitempty"`
	Metadata            map[string]string      `json:"metadata,omitempty"`
}

func (r *UpdateBillingEntityRequest) Validate() error {
	return validator.ValidateRequest(r)
}

type BillingEntityResponse struct {
	*billingentity.BillingEntity
}

type ListBillingEntitiesResponse = types.ListResponse[*BillingEntityResponse]

// BillingEntityModeResponse is the tenant's billing entities state.
type BillingEntityModeResponse struct {
	Mode types.BillingEntityMode `json:"mode"`
}
