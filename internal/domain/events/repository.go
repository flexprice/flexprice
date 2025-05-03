package events

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type Repository interface {
	InsertEvent(ctx context.Context, event *Event) error
	BulkInsertEvents(ctx context.Context, events []*Event) error
	GetUsage(ctx context.Context, params *UsageParams) (*AggregationResult, error)
	GetUsageWithFilters(ctx context.Context, params *UsageWithFiltersParams) ([]*AggregationResult, error)
	GetEvents(ctx context.Context, params *GetEventsParams) ([]*Event, uint64, error)
}

// ProcessedEventRepository defines operations for processed events
type ProcessedEventRepository interface {
	InsertProcessedEvent(ctx context.Context, event *ProcessedEvent) error
	BulkInsertProcessedEvents(ctx context.Context, events []*ProcessedEvent) error
	GetProcessedEvents(ctx context.Context, params *GetProcessedEventsParams) ([]*ProcessedEvent, uint64, error)
	GetUsageSummary(ctx context.Context, params *UsageSummaryParams) (decimal.Decimal, error)
	FindUnprocessedEvents(ctx context.Context, customerID, subscriptionID string) ([]*ProcessedEvent, error)
}

type UsageParams struct {
	ExternalCustomerID string                `json:"external_customer_id"`
	CustomerID         string                `json:"customer_id"`
	EventName          string                `json:"event_name" validate:"required"`
	PropertyName       string                `json:"property_name" validate:"required"`
	AggregationType    types.AggregationType `json:"aggregation_type" validate:"required"`
	WindowSize         types.WindowSize      `json:"window_size"`
	StartTime          time.Time             `json:"start_time" validate:"required"`
	EndTime            time.Time             `json:"end_time" validate:"required"`
	Filters            map[string][]string   `json:"filters"`
}

// UsageSummaryParams defines parameters for querying pre-computed usage
type UsageSummaryParams struct {
	StartTime      time.Time `json:"start_time" validate:"required"`
	EndTime        time.Time `json:"end_time" validate:"required"`
	CustomerID     string    `json:"customer_id"`
	SubscriptionID string    `json:"subscription_id"`
	MeterID        string    `json:"meter_id"`
	PriceID        string    `json:"price_id"`
	FeatureID      string    `json:"feature_id"`
}

// GetProcessedEventsParams defines parameters for querying processed events
type GetProcessedEventsParams struct {
	StartTime      time.Time         `json:"start_time" validate:"required"`
	EndTime        time.Time         `json:"end_time" validate:"required"`
	CustomerID     string            `json:"customer_id"`
	SubscriptionID string            `json:"subscription_id"`
	MeterID        string            `json:"meter_id"`
	FeatureID      string            `json:"feature_id"`
	PriceID        string            `json:"price_id"`
	EventStatus    types.EventStatus `json:"event_status"`
	Offset         int               `json:"offset"`
	Limit          int               `json:"limit"`
	CountTotal     bool              `json:"count_total"`
}

type GetEventsParams struct {
	ExternalCustomerID string              `json:"external_customer_id"`
	EventName          string              `json:"event_name" validate:"required"`
	EventID            string              `json:"event_id"`
	StartTime          time.Time           `json:"start_time" validate:"required"`
	EndTime            time.Time           `json:"end_time" validate:"required"`
	IterFirst          *EventIterator      `json:"iter_first"`
	IterLast           *EventIterator      `json:"iter_last"`
	PageSize           int                 `json:"page_size"`
	PropertyFilters    map[string][]string `json:"property_filters,omitempty"`
	Offset             int                 `json:"offset"`
	Source             string              `json:"source"`
	CountTotal         bool                `json:"count_total"`
}

type UsageResult struct {
	WindowSize time.Time       `json:"window_size"`
	Value      decimal.Decimal `json:"value"`
}

type AggregationResult struct {
	Results   []UsageResult         `json:"results,omitempty"`
	Value     decimal.Decimal       `json:"value,omitempty"`
	EventName string                `json:"event_name"`
	Type      types.AggregationType `json:"type"`
	Metadata  map[string]string     `json:"metadata,omitempty"`
	MeterID   string                `json:"meter_id"`
	PriceID   string                `json:"price_id"`
}

type EventIterator struct {
	Timestamp time.Time
	ID        string
}

// FilterGroup represents a group of filters with priority
type FilterGroup struct {
	// ID is the identifier for the filter group. We are using the price ID
	// as the unique identifier for the filter group as of now
	ID string `json:"id"`

	// Priority is the priority of the filter group for deduping events matching multiple filter groups
	Priority int `json:"priority"`

	// Filters are the actual filters where the key is the $properties.key
	// and the values are all the predefined filter values
	Filters map[string][]string `json:"filters"`
}

type UsageWithFiltersParams struct {
	*UsageParams
	FilterGroups []FilterGroup // Ordered list of filter groups, from most specific to least specific
}
