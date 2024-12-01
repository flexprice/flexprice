package meter

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
)

type Meter struct {
	ID            string      `db:"id" json:"id"`
	EventName     string      `db:"event_name" json:"event_name"`
	Aggregation   Aggregation `db:"aggregation" json:"aggregation"`
	EnvironmentID string      `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

type Aggregation struct {
	Type  types.AggregationType `json:"type"`
	Field string                `json:"field,omitempty"`
}

// Validate validates the meter configuration
func (m *Meter) Validate() error {
	if m.ID == "" {
		return fmt.Errorf("id is required")
	}
	if m.EventName == "" {
		return fmt.Errorf("event_name is required")
	}
	if !m.Aggregation.Type.Validate() {
		return fmt.Errorf("invalid aggregation type: %s", m.Aggregation.Type)
	}
	if m.Aggregation.Type.RequiresField() && m.Aggregation.Field == "" {
		return fmt.Errorf("field is required for aggregation type: %s", m.Aggregation.Type)
	}
	return nil
}

// Constructor for creating new meters with defaults
func NewMeter(id string, tenantID, environmentID, createdBy string) *Meter {
	now := time.Now().UTC()
	if id == "" {
		id = uuid.New().String()
	}

	return &Meter{
		ID:            id,
		EnvironmentID: environmentID,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: createdBy,
			UpdatedBy: createdBy,
			Status:    types.StatusActive,
		},
	}
}
