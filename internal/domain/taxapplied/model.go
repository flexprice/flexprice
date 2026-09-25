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
	ID string `json:"id,omitempty"`
	// Null when an external engine calculated the tax: there is no Flexprice rate to point at.
	TaxRateID        *string                 `json:"tax_rate_id,omitempty"`
	EntityType       types.TaxRateEntityType `json:"entity_type,omitempty"`
	EntityID         string                  `json:"entity_id,omitempty"`
	TaxAssociationID *string                 `json:"tax_association_id,omitempty"`
	TaxableAmount    decimal.Decimal         `json:"taxable_amount,omitempty" swaggertype:"string"`
	TaxAmount        decimal.Decimal         `json:"tax_amount,omitempty" swaggertype:"string"`
	TaxBehavior      types.TaxBehavior       `json:"tax_behavior,omitempty"`
	Currency         string                  `json:"currency,omitempty"`
	AppliedAt        time.Time               `json:"applied_at,omitempty"`
	EnvironmentID    string                  `json:"environment_id,omitempty"`
	Metadata         map[string]string       `json:"metadata,omitempty"`
	IdempotencyKey   *string                 `json:"idempotency_key,omitempty"`
	// Provider is the engine that produced this row. Empty means the native engine.
	Provider types.TaxProvider `json:"provider,omitempty"`
	// TaxTransactionID is the provider transaction recording the filed tax, stamped at commit.
	TaxTransactionID *string `json:"tax_transaction_id,omitempty"`
	// TaxTransactionType is what the provider transaction did. Empty means a filing.
	TaxTransactionType types.TaxTransactionType `json:"tax_transaction_type,omitempty"`
	// ExternalTaxDetails is what the engine returned about this tax. Null on a native row.
	ExternalTaxDetails *types.ExternalTaxDetails `json:"external_tax_details,omitempty"`
	types.BaseModel
}

// IsReversal reports whether this row records tax being un-filed rather than filed. Nothing
// that totals or renders an entity's tax may count one.
func (t *TaxApplied) IsReversal() bool {
	if t == nil {
		return false
	}
	return t.TaxTransactionType.IsReversal()
}

// IsExternal reports whether an engine outside Flexprice produced this row.
func (t *TaxApplied) IsExternal() bool {
	if t == nil {
		return false
	}
	return t.Provider.IsExternal()
}

// GetTaxRateID returns the Flexprice rate this row came from, or an empty string when an
// external engine produced it.
func (t *TaxApplied) GetTaxRateID() string {
	if t == nil || t.TaxRateID == nil {
		return ""
	}
	return *t.TaxRateID
}

func FromEnt(ent *ent.TaxApplied) *TaxApplied {
	// Rows written before tax_behavior existed have none, and exclusive is the only
	// behavior that was ever charged back then.
	taxBehavior := ent.TaxBehavior
	if taxBehavior == "" {
		taxBehavior = types.TaxBehaviorExclusive
	}

	return &TaxApplied{
		ID:                 ent.ID,
		TaxRateID:          ent.TaxRateID,
		EntityType:         types.TaxRateEntityType(ent.EntityType),
		EntityID:           ent.EntityID,
		TaxAssociationID:   ent.TaxAssociationID,
		TaxableAmount:      ent.TaxableAmount,
		TaxAmount:          ent.TaxAmount,
		TaxBehavior:        taxBehavior,
		Currency:           ent.Currency,
		AppliedAt:          ent.AppliedAt,
		EnvironmentID:      ent.EnvironmentID,
		Metadata:           ent.Metadata,
		IdempotencyKey:     ent.IdempotencyKey,
		Provider:           ent.Provider,
		TaxTransactionID:   ent.TaxTransactionID,
		TaxTransactionType: ent.TaxTransactionType,
		ExternalTaxDetails: ent.ExternalTaxDetails,
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
