package meter

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/google/uuid"
)

type Meter struct {
	ID          string           `db:"id" json:"id"`
	EventName   string           `db:"event_name" json:"event_name"`
	Name        string           `db:"name" json:"name"`
	Aggregation Aggregation      `db:"aggregation" json:"aggregation"`
	Filters     []Filter         `db:"filters" json:"filters"`
	ResetUsage  types.ResetUsage `db:"reset_usage" json:"reset_usage"`
	types.BaseModel
}

type Filter struct {
	Key    string   `json:"key"`
	Values []string `json:"values"`
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
	if m.Name == "" {
		return fmt.Errorf("name is required")
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

	for _, filter := range m.Filters {
		if filter.Key == "" {
			return fmt.Errorf("filter key cannot be empty")
		}
		if len(filter.Values) == 0 {
			return fmt.Errorf("filter values cannot be empty for key: %s", filter.Key)
		}
	}
	return nil
}

// Constructor for creating new meters with defaults
func NewMeter(name string, tenantID, createdBy string) *Meter {
	now := time.Now().UTC()
	return &Meter{
		ID:   uuid.New().String(),
		Name: name,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: createdBy,
			UpdatedBy: createdBy,
		},
		Filters:    []Filter{},
		ResetUsage: types.ResetUsageBillingPeriod,
	}
}
