package dto

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

type CreateTaxAppliedRequest struct {
	TaxRateID        string                  `json:"tax_rate_id" validate:"required"`
	EntityType       types.TaxrateEntityType `json:"entity_type" validate:"required"`
	EntityID         string                  `json:"entity_id" validate:"required"`
	TaxableAmount    decimal.Decimal         `json:"taxable_amount" validate:"required"`
	TaxAmount        decimal.Decimal         `json:"tax_amount" validate:"required"`
	Currency         string                  `json:"currency" validate:"required"`
	TaxAssociationID *string                 `json:"tax_association_id,omitempty"`
	Metadata         map[string]string       `json:"metadata,omitempty"`
}

func (r *CreateTaxAppliedRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}
	return nil
}

func (r *CreateTaxAppliedRequest) ToTaxApplied(ctx context.Context) *taxapplied.TaxApplied {
	return &taxapplied.TaxApplied{
		ID:               types.GenerateUUIDWithPrefix(types.UUID_PREFIX_TAX_APPLIED),
		TaxRateID:        r.TaxRateID,
		EntityType:       r.EntityType,
		EntityID:         r.EntityID,
		TaxableAmount:    r.TaxableAmount,
		TaxAmount:        r.TaxAmount,
		Currency:         r.Currency,
		TaxAssociationID: r.TaxAssociationID,
		Metadata:         r.Metadata,
		EnvironmentID:    types.GetEnvironmentID(ctx),
		BaseModel:        types.GetDefaultBaseModel(ctx),
	}
}
