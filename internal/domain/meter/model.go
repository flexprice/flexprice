package meter

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/schema"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
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

	// EnvironmentID is the environment identifier for the meter
	EnvironmentID string `db:"environment_id" json:"environment_id"`

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
	Type types.AggregationType `json:"type"`

	// Field is the key in $event.properties on which the aggregation is to be applied
	// For ex if the aggregation type is sum for API usage, the field could be "duration_ms"
	Field string `json:"field,omitempty"`

	// Multiplier is the multiplier for the aggregation
	// For ex if the aggregation type is sum_with_multiplier for API usage, the multiplier could be 1000
	// to scale up by a factor of 1000. If not provided, it will be null.
	Multiplier *decimal.Decimal `json:"multiplier,omitempty"`

	// BucketSize is used only for MAX aggregation when windowed aggregation is needed
	// It defines the size of time windows to calculate max values within
	BucketSize types.WindowSize `json:"bucket_size,omitempty"`
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
			Type:       e.Aggregation.Type,
			Field:      e.Aggregation.Field,
			Multiplier: e.Aggregation.Multiplier,
			BucketSize: e.Aggregation.BucketSize,
		},
		Filters:       filters,
		ResetUsage:    types.ResetUsage(e.ResetUsage),
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
		Type:       m.Aggregation.Type,
		Field:      m.Aggregation.Field,
		Multiplier: m.Aggregation.Multiplier,
		BucketSize: m.Aggregation.BucketSize,
	}
}

// Validate validates the meter configuration
func (m *Meter) Validate() error {
	if m.ID == "" {
		return ierr.NewError("id is required").
			WithHint("Please provide a valid meter ID").
			Mark(ierr.ErrValidation)
	}
	if m.Name == "" {
		return ierr.NewError("name is required").
			WithHint("Please provide a name for the meter").
			Mark(ierr.ErrValidation)
	}
	if m.EventName == "" {
		return ierr.NewError("event_name is required").
			WithHint("Please specify the event name to track").
			Mark(ierr.ErrValidation)
	}
	if !m.Aggregation.Type.Validate() {
		return ierr.NewError("invalid aggregation type").
			WithHint("Please provide a valid aggregation type").
			WithReportableDetails(map[string]interface{}{
				"aggregation_type": m.Aggregation.Type,
			}).
			Mark(ierr.ErrValidation)
	}
	if m.Aggregation.Type.RequiresField() && m.Aggregation.Field == "" {
		return ierr.NewError("field is required for aggregation type").
			WithHint("Please specify a field for this aggregation type").
			WithReportableDetails(map[string]interface{}{
				"aggregation_type": m.Aggregation.Type,
			}).
			Mark(ierr.ErrValidation)
	}
	if m.Aggregation.Type == types.AggregationSumWithMultiplier {
		if m.Aggregation.Multiplier == nil {
			return ierr.NewError("multiplier is required for SUM_WITH_MULTIPLIER").
				WithHint("Please provide a multiplier value").
				Mark(ierr.ErrValidation)
		}
		if m.Aggregation.Multiplier.LessThanOrEqual(decimal.NewFromFloat(0)) {
			return ierr.NewError("invalid multiplier value").
				WithHint("Multiplier must be greater than zero").
				WithReportableDetails(map[string]interface{}{
					"multiplier": m.Aggregation.Multiplier,
				}).
				Mark(ierr.ErrValidation)
		}
	}
	// Validate bucket_size is only used with MAX aggregation
	if m.Aggregation.BucketSize != "" && m.Aggregation.Type != types.AggregationMax {
		return ierr.NewError("bucket_size can only be used with MAX aggregation").
			WithHint("BucketSize is only valid for MAX aggregation type").
			WithReportableDetails(map[string]interface{}{
				"aggregation_type": m.Aggregation.Type,
				"bucket_size":      m.Aggregation.BucketSize,
			}).
			Mark(ierr.ErrValidation)
	}
	// If bucket_size is provided for MAX aggregation, validate it's a valid window size
	if m.Aggregation.Type == types.AggregationMax && m.Aggregation.BucketSize != "" {
		if err := m.Aggregation.BucketSize.Validate(); err != nil {
			return ierr.NewError("invalid bucket_size").
				WithHint("Please provide a valid window size for bucket_size").
				WithReportableDetails(map[string]interface{}{
					"bucket_size": m.Aggregation.BucketSize,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	for _, filter := range m.Filters {
		if filter.Key == "" {
			return ierr.NewError("filter key cannot be empty").
				WithHint("Please provide a key for each filter").
				Mark(ierr.ErrValidation)
		}
		if len(filter.Values) == 0 {
			return ierr.NewError("filter values cannot be empty").
				WithHint("Please provide at least one value for each filter").
				WithReportableDetails(map[string]interface{}{
					"filter_key": filter.Key,
				}).
				Mark(ierr.ErrValidation)
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
