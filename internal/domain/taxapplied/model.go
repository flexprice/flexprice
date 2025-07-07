package taxapplied

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// TaxApplied is the model entity for the TaxApplied schema.
type TaxApplied struct {
	ID               string                  `json:"id,omitempty"`
	TaxRateID        string                  `json:"tax_rate_id,omitempty"`
	EntityType       types.TaxrateEntityType `json:"entity_type,omitempty"`
	EntityID         string                  `json:"entity_id,omitempty"`
	TaxAssociationID *string                 `json:"tax_association_id,omitempty"`
	TaxableAmount    decimal.Decimal         `json:"taxable_amount,omitempty"`
	TaxAmount        decimal.Decimal         `json:"tax_amount,omitempty"`
	Currency         string                  `json:"currency,omitempty"`
	Jurisdiction     string                  `json:"jurisdiction,omitempty"`
	AppliedAt        time.Time               `json:"applied_at,omitempty"`
	EnvironmentID    string                  `json:"environment_id,omitempty"`
	Metadata         map[string]string       `json:"metadata,omitempty"`

	types.BaseModel
}

func FromEnt(ent *ent.TaxApplied) *TaxApplied {
	return &TaxApplied{
		ID:               ent.ID,
		TaxRateID:        ent.TaxRateID,
		EntityType:       types.TaxrateEntityType(ent.EntityType),
		EntityID:         ent.EntityID,
		TaxAssociationID: ent.TaxAssociationID,
		TaxableAmount:    ent.TaxableAmount,
		TaxAmount:        ent.TaxAmount,
		Currency:         ent.Currency,
		Jurisdiction:     ent.Jurisdiction,
		AppliedAt:        ent.AppliedAt,
		EnvironmentID:    ent.EnvironmentID,
		Metadata:         ent.Metadata,
		BaseModel: types.BaseModel{
			TenantID:  ent.TenantID,
			Status:    types.Status(ent.Status),
			CreatedAt: ent.CreatedAt,
			UpdatedAt: ent.UpdatedAt,
			CreatedBy: ent.CreatedBy,
			UpdatedBy: ent.UpdatedBy,
		},
	}
}

func FromEntList(ents []*ent.TaxApplied) []*TaxApplied {
	return lo.Map(ents, func(ent *ent.TaxApplied, _ int) *TaxApplied {
		return FromEnt(ent)
	})
}
