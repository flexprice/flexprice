package addon

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Addon represents an addon in the domain
type Addon struct {
	ID            string                 `json:"id,omitempty"`
	LookupKey     string                 `json:"lookup_key,omitempty"`
	Name          string                 `json:"name,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Type          types.AddonType        `json:"type,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	EnvironmentID string                 `json:"environment_id,omitempty"`
	types.BaseModel
}

// FromEnt creates domain model from ent model
func (a *Addon) FromEnt(entAddon *ent.Addon) *Addon {
	addon := &Addon{
		ID:            entAddon.ID,
		LookupKey:     entAddon.LookupKey,
		Name:          entAddon.Name,
		Description:   entAddon.Description,
		Type:          types.AddonType(entAddon.Type),
		Metadata:      entAddon.Metadata,
		EnvironmentID: entAddon.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  entAddon.TenantID,
			Status:    types.Status(entAddon.Status),
			CreatedAt: entAddon.CreatedAt,
			UpdatedAt: entAddon.UpdatedAt,
			CreatedBy: entAddon.CreatedBy,
			UpdatedBy: entAddon.UpdatedBy,
		},
	}

	return addon
}

func (a *Addon) FromEntList(entAddons []*ent.Addon) []*Addon {
	return lo.Map(entAddons, func(entAddon *ent.Addon, _ int) *Addon {
		return a.FromEnt(entAddon)
	})
}
