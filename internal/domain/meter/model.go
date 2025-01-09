package meter

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/types"
)

type Meter struct {
	// ID is the unique identifier for the meter
	ID string `db:"id" json:"id"`

	// EventName is the unique identifier for the event that this meter is tracking
	// It is a mandatory field in the events table and hence being used as the primary matching field
	// We can have multiple meters tracking the same event but with different filters and aggregation
	EventName string `db:"event_name" json:"event_name"`

	// Name is the display name of the meter
	Name string `db:"name" json:"name"`

	// Aggregation defines the aggregation type and field for the meter
	// It is used to aggregate the events into a single value for calculating the usage
	Aggregation Aggregation `db:"aggregation" json:"aggregation"`

	// Filters define the criteria for the meter to be applied on the events before aggregation
	// It also defines the possible values on which later the charges will be applied
	Filters []Filter `db:"filters" json:"filters"`

	// ResetUsage defines whether the usage should be reset periodically or not
	// For ex meters tracking total storage used do not get reset but meters tracking
	// total API requests do.
	ResetUsage types.ResetUsage `db:"reset_usage" json:"reset_usage"`

	// BaseModel is the base model for the meter
	types.BaseModel
}

type Filter struct {
	// Key is the key for the filter from $event.properties
	// Currently we support only first level keys in the properties and not nested keys
	Key string `json:"key"`

	// Values are the possible values for the filter to be considered for the meter
	// For ex "model_name" could have values "o1-mini", "gpt-4o" etc
	Values []string `json:"values"`
}

type Aggregation struct {
	// Type is the type of aggregation to be applied on the events
	// For ex sum, count, avg, max, min etc
	Type types.AggregationType `json:"type"`

	// Field is the key in $event.properties on which the aggregation is to be applied
	// For ex if the aggregation type is sum for API usage, the field could be "duration_ms"
	Field string `json:"field,omitempty"`
}

// FromEnt converts an Ent Meter to a domain Meter
func FromEnt(e *ent.Meter) *Meter {
	if e == nil {
		return nil
	}

	// Convert filters from schema to domain model
	filters := make([]Filter, len(e.Filters))
	for i, f := range e.Filters {
		filters[i] = Filter{
			Key:    f.Key,
			Values: f.Values,
		}
	}

	return &Meter{
		ID:        e.ID,
		EventName: e.EventName,
		Name:      e.Name,
		Aggregation: Aggregation{
			Type:  e.Aggregation.Type,
			Field: e.Aggregation.Field,
		},
		Filters:    filters,
		ResetUsage: types.ResetUsage(e.ResetUsage),
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

// FromEntList converts a list of Ent Meters to domain Meters
func FromEntList(list []*ent.Meter) []*Meter {
	if list == nil {
		return nil
	}
	meters := make([]*Meter, len(list))
	for i, item := range list {
		meters[i] = FromEnt(item)
	}
	return meters
}

// ToEntFilters converts domain Filters to Ent Filters
func (m *Meter) ToEntFilters() []schema.MeterFilter {
	if len(m.Filters) == 0 {
		return nil
	}
	filters := make([]schema.MeterFilter, len(m.Filters))
	for i, f := range m.Filters {
		filters[i] = schema.MeterFilter{
			Key:    f.Key,
			Values: f.Values,
		}
	}
	return filters
}

// ToEntAggregation converts domain Aggregation to Ent Aggregation
func (m *Meter) ToEntAggregation() schema.MeterAggregation {
	return schema.MeterAggregation{
		Type:  m.Aggregation.Type,
		Field: m.Aggregation.Field,
	}
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
		ID:   types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER),
		Name: name,
		BaseModel: types.BaseModel{
			TenantID:  tenantID,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: createdBy,
			UpdatedBy: createdBy,
			Status:    types.StatusPublished,
		},
		Filters:    []Filter{},
		ResetUsage: types.ResetUsageBillingPeriod,
	}
}
