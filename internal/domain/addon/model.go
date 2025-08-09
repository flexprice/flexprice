package addon

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type Addon struct {
	ID            string                 `json:"id,omitempty"`
	EnvironmentID string                 `json:"environment_id,omitempty"`
	LookupKey     string                 `json:"lookup_key,omitempty"`
	Name          string                 `json:"name,omitempty"`
	Description   string                 `json:"description,omitempty"`
	Type          types.AddonType        `json:"type,omitempty"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`

	types.BaseModel
}

func FromEnt(ent *ent.Addon) *Addon {
	return &Addon{
		ID:            ent.ID,
		EnvironmentID: ent.EnvironmentID,
		LookupKey:     ent.LookupKey,
		Name:          ent.Name,
		Description:   ent.Description,
		Type:          types.AddonType(ent.Type),
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

func FromEntList(ents []*ent.Addon) []*Addon {
	return lo.Map(ents, func(ent *ent.Addon, _ int) *Addon {
		return FromEnt(ent)
	})
}
