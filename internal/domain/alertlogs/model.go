package alertlogs

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// AlertLog represents an alert log entry for monitoring entity states
type AlertLog struct {
	ID            string                `db:"id" json:"id"`
	EntityType    types.AlertEntityType `db:"entity_type" json:"entity_type"`
	EntityID      string                `db:"entity_id" json:"entity_id"`
	AlertType     types.AlertType       `db:"alert_type" json:"alert_type"`
	AlertStatus   types.AlertState      `db:"alert_status" json:"alert_status"`
	AlertInfo     types.AlertInfo       `db:"alert_info" json:"alert_info"`
	EnvironmentID string                `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an Ent AlertLog to a domain AlertLog
func FromEnt(e *ent.AlertLogs) *AlertLog {
	if e == nil {
		return nil
	}
	return &AlertLog{
		ID:            e.ID,
		EntityType:    types.AlertEntityType(e.EntityType),
		EntityID:      e.EntityID,
		AlertType:     types.AlertType(e.AlertType),
		AlertStatus:   types.AlertState(e.AlertStatus),
		AlertInfo:     e.AlertInfo,
		EnvironmentID: e.EnvironmentID,
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

// FromEntList converts a list of Ent AlertLogs to domain AlertLogs
func FromEntList(list []*ent.AlertLogs) []*AlertLog {
	if list == nil {
		return nil
	}
	alertLogs := make([]*AlertLog, len(list))
	for i, item := range list {
		alertLogs[i] = FromEnt(item)
	}
	return alertLogs
}
