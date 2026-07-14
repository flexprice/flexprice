package alert

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// AlertSettings represents a single row in the alert_settings table: the threshold
// configuration for one monitored entity (subscription, subscription line item, or group).
type AlertSettings struct {
	ID               string                `db:"id" json:"id"`
	EntityType       types.AlertEntityType `db:"entity_type" json:"entity_type"`
	EntityID         string                `db:"entity_id" json:"entity_id"`
	ParentEntityType *string               `db:"parent_entity_type" json:"parent_entity_type,omitempty"`
	ParentEntityID   *string               `db:"parent_entity_id" json:"parent_entity_id,omitempty"`
	Enabled          bool                  `db:"enabled" json:"enabled"`
	Config           *types.AlertSettings  `db:"config" json:"config"`
	EnvironmentID    string                `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an Ent AlertSettings to a domain AlertSettings
func FromEnt(e *ent.AlertSettings) *AlertSettings {
	if e == nil {
		return nil
	}
	return &AlertSettings{
		ID:               e.ID,
		EntityType:       e.EntityType,
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
