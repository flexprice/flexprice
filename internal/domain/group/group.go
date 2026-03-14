package group

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// Group represents a grouping entity for organizing related items
type Group struct {
	ID            string                `json:"id"`
	Name          string                `json:"name"`
	EntityType    types.GroupEntityType `json:"entity_type"`
	EnvironmentID string                `json:"environment_id"`
	LookupKey     string                `json:"lookup_key,omitempty"`
	Metadata      map[string]string     `json:"metadata,omitempty"`
	types.BaseModel
}

// FromEnt converts an Ent Group to a domain Group
func FromEnt(e *ent.Group) *Group {
	if e == nil {
		return nil
	}
	return &Group{
		ID:            e.ID,
		Name:          e.Name,
		EntityType:    types.GroupEntityType(e.EntityType),
		EnvironmentID: e.EnvironmentID,
		LookupKey:     e.LookupKey,
		Metadata:      e.Metadata,
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

// FromEntList converts a list of Ent Groups to domain Groups
func FromEntList(list []*ent.Group) []*Group {
	if list == nil {
		return nil
	}
	groups := make([]*Group, len(list))
	for i, item := range list {
		groups[i] = FromEnt(item)
	}
	return groups
}
