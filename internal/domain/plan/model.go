package plan

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type Plan struct {
	ID            string         `db:"id" json:"id"`
	Name          string         `db:"name" json:"name"`
	LookupKey     string         `db:"lookup_key" json:"lookup_key"`
	Description   string         `db:"description" json:"description"`
	EnvironmentID string         `db:"environment_id" json:"environment_id"`
	Metadata      types.Metadata `db:"metadata" json:"metadata"`
	DisplayOrder  *int           `db:"display_order" json:"display_order,omitempty"`
	types.BaseModel
}

// FromEnt converts an Ent Plan to a domain Plan
func FromEnt(e *ent.Plan) *Plan {
	if e == nil {
		return nil
	}
	return &Plan{
		ID:            e.ID,
		Name:          e.Name,
		LookupKey:     e.LookupKey,
		Description:   e.Description,
		EnvironmentID: e.EnvironmentID,
		Metadata:      types.Metadata(e.Metadata),
		DisplayOrder:  &e.DisplayOrder,
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

// FromEntList converts a list of Ent Plans to domain Plans
func FromEntList(list []*ent.Plan) []*Plan {
	if list == nil {
		return nil
	}
	plans := make([]*Plan, len(list))
	for i, item := range list {
		plans[i] = FromEnt(item)
	}
	return plans
}
