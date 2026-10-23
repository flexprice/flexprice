package alert

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// AlertSettings represents a single row in the alert_settings table: the threshold
// configuration for one monitored entity (subscription, subscription line item, or group).
type AlertSettings struct {
	ID               string                `json:"id"`
	EntityType       types.AlertEntityType `json:"entity_type"`
	EntityID         string                `json:"entity_id"`
	ParentEntityType *string               `json:"parent_entity_type,omitempty"`
	ParentEntityID   *string               `json:"parent_entity_id,omitempty"`
	Enabled          bool                  `json:"enabled"`
	Config           *types.AlertSettings  `json:"config"`
	EnvironmentID    string                `json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an Ent AlertSettings to a domain AlertSettings
func FromEnt(e *ent.AlertSettings) *AlertSettings {
	if e == nil {
		return nil
	}
	return &AlertSettings{
		ID:               e.ID,
		EntityType:       types.AlertEntityType(e.EntityType),
		EntityID:         e.EntityID,
		ParentEntityType: (*string)(e.ParentEntityType),
		ParentEntityID:   e.ParentEntityID,
		Enabled:          e.Enabled,
		Config:           &e.Config,
		EnvironmentID:    e.EnvironmentID,
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

// FromEntList converts a list of Ent AlertSettings to domain AlertSettings
func FromEntList(list []*ent.AlertSettings) []*AlertSettings {
	if list == nil {
		return nil
	}
	alertSettingsList := make([]*AlertSettings, len(list))
	for i, item := range list {
		alertSettingsList[i] = FromEnt(item)
	}
	return alertSettingsList
}
