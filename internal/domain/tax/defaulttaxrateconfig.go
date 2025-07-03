package taxrate

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// DefaultTaxRateConfig is the model entity for the DefaultTaxRateConfig schema.
type DefaultTaxRateConfig struct {
	// ID of the ent.
	ID string `json:"id,omitempty"`
	// Reference to the TaxRate entity
	TaxRateID string `json:"tax_rate_id,omitempty"`
	// Type of entity this tax rate applies to
	EntityType string `json:"entity_type,omitempty"`
	// ID of the entity this tax rate applies to
	EntityID string `json:"entity_id,omitempty"`
	// Priority for tax resolution (lower number = higher priority)
	Priority int `json:"priority,omitempty"`
	// Whether this tax should be automatically applied
	AutoApply bool `json:"auto_apply,omitempty"`
	// Start date for this tax assignment
	ValidFrom *time.Time `json:"valid_from,omitempty"`
	// End date for this tax assignment
	ValidTo *time.Time `json:"valid_to,omitempty"`
	// Currency
	Currency string `json:"currency,omitempty"`
	// Metadata holds the value of the "metadata" field.
	Metadata map[string]string `json:"metadata,omitempty"`

	// EnvironmentID is the ID of the environment this tax rate config belongs to
	EnvironmentID string `json:"environment_id,omitempty"`

	types.BaseModel
}

func (d *DefaultTaxRateConfig) FromEnt(ent *ent.DefaultTaxRateConfig) *DefaultTaxRateConfig {
	return &DefaultTaxRateConfig{
		ID:            ent.ID,
		TaxRateID:     ent.TaxRateID,
		EntityType:    ent.EntityType,
		EntityID:      ent.EntityID,
		Priority:      ent.Priority,
		AutoApply:     ent.AutoApply,
		ValidFrom:     ent.ValidFrom,
		ValidTo:       ent.ValidTo,
		Currency:      ent.Currency,
		EnvironmentID: ent.EnvironmentID,
		Metadata:      ent.Metadata,
		BaseModel: types.BaseModel{
			CreatedAt: ent.CreatedAt,
			UpdatedAt: ent.UpdatedAt,
			CreatedBy: ent.CreatedBy,
			UpdatedBy: ent.UpdatedBy,
		},
	}
}

func (d *DefaultTaxRateConfig) FromEntList(ents []*ent.DefaultTaxRateConfig) []*DefaultTaxRateConfig {
	var configs []*DefaultTaxRateConfig
	for _, ent := range ents {
		configs = append(configs, d.FromEnt(ent))
	}
	return configs
}
