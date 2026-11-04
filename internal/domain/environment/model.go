package environment

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type Environment struct {
	ID   string                `db:"id" json:"id"`
	Name string                `db:"name" json:"name"`
	Type types.EnvironmentType `db:"type" json:"type"`

	types.BaseModel
}

// FromEnt converts an ent Environment to a domain Environment
func FromEnt(e *ent.Environment) *Environment {
	if e == nil {
		return nil
	}

	return &Environment{
		ID:   e.ID,
		Name: e.Name,
		Type: types.EnvironmentType(e.Type),
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// FromEntList converts a list of ent Environments to domain Environments
func FromEntList(environments []*ent.Environment) []*Environment {
	if environments == nil {
		return nil
	}

	result := make([]*Environment, len(environments))
	for i, e := range environments {
		result[i] = FromEnt(e)
	}

	return result
}
