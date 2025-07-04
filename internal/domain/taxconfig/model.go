package taxconfig

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// TaxConfig is the model entity for the TaxConfig schema.
type TaxConfig struct {
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
	// Currency
	Currency string `json:"currency,omitempty"`
	// Metadata holds the value of the "metadata" field.
	Metadata map[string]string `json:"metadata,omitempty"`

	// EnvironmentID is the ID of the environment this tax rate config belongs to
	EnvironmentID string `json:"environment_id,omitempty"`

	types.BaseModel
}

func FromEnt(ent *ent.TaxConfig) *TaxConfig {
	return &TaxConfig{
		ID:            ent.ID,
		TaxRateID:     ent.TaxRateID,
		EntityType:    ent.EntityType,
		EntityID:      ent.EntityID,
		Priority:      ent.Priority,
		AutoApply:     ent.AutoApply,
		Currency:      ent.Currency,
		EnvironmentID: ent.EnvironmentID,
		Metadata:      ent.Metadata,
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

func FromEntList(ents []*ent.TaxConfig) []*TaxConfig {
	var configs []*TaxConfig
	for _, ent := range ents {
		configs = append(configs, FromEnt(ent))
	}
	return configs
}
