package events

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type Repository interface {
	InsertEvent(ctx context.Context, event *Event) error
	GetUsage(ctx context.Context, params *UsageParams) (*AggregationResult, error)
	GetUsageWithFilters(ctx context.Context, params *UsageWithFiltersParams) ([]*AggregationResult, error)
	GetEvents(ctx context.Context, params *GetEventsParams) ([]*Event, error)
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

type GetEventsParams struct {
	ExternalCustomerID string         `json:"external_customer_id"`
	EventName          string         `json:"event_name" validate:"required"`
	EventID            string         `json:"event_id"`
	StartTime          time.Time      `json:"start_time" validate:"required"`
	EndTime            time.Time      `json:"end_time" validate:"required"`
	IterFirst          *EventIterator `json:"iter_first"`
	IterLast           *EventIterator `json:"iter_last"`
	PageSize           int            `json:"page_size"`
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
