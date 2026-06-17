package taxassociation

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// TaxAssociation is the model entity for the TaxAssociation schema.
type TaxAssociation struct {
	// ID of the ent.
	ID string `json:"id,omitempty"`
	// Reference to the TaxRate entity
	TaxRateID string `json:"tax_rate_id,omitempty"`
	// Type of entity this tax rate applies to
	EntityType types.TaxRateEntityType `json:"entity_type,omitempty"`
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
	// StartDate is the date from which this association is active
	StartDate time.Time `json:"start_date,omitempty"`
	// EndDate is the optional date until which this association is active
	EndDate *time.Time `json:"end_date,omitempty"`

	// EnvironmentID is the ID of the environment this tax rate config belongs to
	EnvironmentID string `json:"environment_id,omitempty"`

	types.BaseModel
}

func FromEnt(ent *ent.TaxAssociation) *TaxAssociation {
	return &TaxAssociation{
		ID:            ent.ID,
		TaxRateID:     ent.TaxRateID,
		EntityType:    types.TaxRateEntityType(ent.EntityType),
		EntityID:      ent.EntityID,
		Priority:      ent.Priority,
		AutoApply:     ent.AutoApply,
		Currency:      ent.Currency,
		EnvironmentID: ent.EnvironmentID,
		Metadata:      ent.Metadata,
		StartDate:     lo.FromPtrOr(ent.StartDate, time.Now().UTC()),
		EndDate:       ent.EndDate,
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

func FromEntList(ents []*ent.TaxAssociation) []*TaxAssociation {
	var configs []*TaxAssociation
	for _, ent := range ents {
		configs = append(configs, FromEnt(ent))
	}
	return configs
}
