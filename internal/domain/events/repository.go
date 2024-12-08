package events

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	InsertEvent(ctx context.Context, event *Event) error
	GetUsage(ctx context.Context, params *UsageParams) (*AggregationResult, error)
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
	StartTime          time.Time      `json:"start_time" validate:"required"`
	EndTime            time.Time      `json:"end_time" validate:"required"`
	IterFirst          *EventIterator `json:"iter_first"`
	IterLast           *EventIterator `json:"iter_last"`
	PageSize           int            `json:"page_size"`
}

type UsageResult struct {
	WindowSize time.Time   `json:"window_size"`
	Value      interface{} `json:"value"`
}
type AggregationResult struct {
	Results   []UsageResult         `json:"results,omitempty"`
	Value     interface{}           `json:"value,omitempty"`
	EventName string                `json:"event_name"`
	Type      types.AggregationType `json:"type"`
}

type EventIterator struct {
	Timestamp time.Time
	ID        string
}
