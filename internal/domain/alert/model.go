package alert

import (
	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Alert represents the domain model for an alert
type Alert struct {
	ID            string                 `json:"id"`
	EntityType    string                 `json:"entity_type"`
	EntityID      string                 `json:"entity_id,omitempty"`
	AlertMetric   types.AlertMetric      `json:"alert_metric"`
	AlertState    types.AlertState       `json:"alert_state"`
	AlertInfo     map[string]interface{} `json:"alert_info,omitempty"`
	EnvironmentID string                 `json:"environment_id"`
	types.BaseModel
}

// FromEnt converts ent.Alert to domain Alert
func FromEnt(a *ent.Alert) *Alert {
	if a == nil {
		return nil
	}

	return &Alert{
		ID:            a.ID,
		EntityType:    a.EntityType,
		EntityID:      lo.FromPtr(a.EntityID),
		AlertMetric:   a.AlertMetric,
		AlertState:    types.AlertState(a.AlertState),
		AlertInfo:     a.AlertInfo,
		EnvironmentID: a.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  a.TenantID,
			Status:    types.Status(a.Status),
			CreatedAt: a.CreatedAt,
			UpdatedAt: a.UpdatedAt,
			CreatedBy: a.CreatedBy,
			UpdatedBy: a.UpdatedBy,
		},
	}
}

// FromEntList converts []*ent.Alert to []*Alert
func FromEntList(alerts []*ent.Alert) []*Alert {
	result := make([]*Alert, len(alerts))
	for i, a := range alerts {
		result[i] = FromEnt(a)
	}
	return result
}

// Validate validates the alert
func (a *Alert) Validate() error {
	if a.EntityType == "" {
		return ierr.NewError("entity_type is required").Mark(ierr.ErrValidation)
	}
	if string(a.AlertMetric) == "" {
		return ierr.NewError("alert_metric is required").Mark(ierr.ErrValidation)
	}
	return nil
}
