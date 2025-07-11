package dto

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
)

type IngestEventRequest struct {
	EventName          string                 `json:"event_name" validate:"required" binding:"required" example:"api_request" csv:"event_name"`
	EventID            string                 `json:"event_id" example:"event123" csv:"event_id"`
	CustomerID         string                 `json:"customer_id" example:"customer456" csv:"customer_id"`
	ExternalCustomerID string                 `json:"external_customer_id" validate:"required" binding:"required" example:"customer456" csv:"external_customer_id"`
	Timestamp          time.Time              `json:"timestamp" example:"2024-03-20T15:04:05Z" csv:"-"` // Handled separately due to parsing
	TimestampStr       string                 `json:"-" csv:"timestamp"`                                // Used for CSV parsing
	Source             string                 `json:"source" example:"api" csv:"source"`
	Properties         map[string]interface{} `json:"properties" swaggertype:"object,string,number" example:"{\"request_size\":100,\"response_status\":200}" csv:"-"` // Handled separately for dynamic columns
}

func (r *IngestEventRequest) Validate() error {
	return validator.ValidateRequest(r)
}

type BulkIngestEventRequest struct {
	Events []*IngestEventRequest `json:"events" validate:"required,min=1,max=1000"`
}

func (r *BulkIngestEventRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *IngestEventRequest) ToEvent(ctx context.Context) *events.Event {
	return events.NewEvent(
		r.EventName,
		types.GetTenantID(ctx),
		r.ExternalCustomerID,
		r.Properties,
		r.Timestamp,
		r.EventID,
		r.CustomerID,
		r.Source,
		types.GetEnvironmentID(ctx),
	)
}

type GetUsageRequest struct {
	ExternalCustomerID string                `form:"external_customer_id" json:"external_customer_id" example:"customer456"`
	CustomerID         string                `form:"customer_id" json:"customer_id" example:"customer456"`
	EventName          string                `form:"event_name" json:"event_name" binding:"required" required:"true" example:"api_request"`
	PropertyName       string                `form:"property_name" json:"property_name" example:"request_size"` // will be empty/ignored in case of COUNT
	AggregationType    types.AggregationType `form:"aggregation_type" json:"aggregation_type" binding:"required"`
	StartTime          time.Time             `form:"start_time" json:"start_time" example:"2024-03-13T00:00:00Z"`
	EndTime            time.Time             `form:"end_time" json:"end_time" example:"2024-03-20T00:00:00Z"`
	WindowSize         types.WindowSize      `form:"window_size" json:"window_size"`
	Filters            map[string][]string   `form:"filters,omitempty" json:"filters,omitempty"`
	PriceID            string                `form:"-" json:"-"` // this is just for internal use to store the price id
	MeterID            string                `form:"-" json:"-"` // this is just for internal use to store the meter id
	Multiplier         *decimal.Decimal      `form:"multiplier" json:"multiplier,omitempty"`
}

type GetUsageByMeterRequest struct {
	MeterID            string              `form:"meter_id" json:"meter_id" binding:"required" example:"123"`
	Meter              *meter.Meter        `form:"-" json:"-"` // caller can set this in case already fetched from db to avoid extra db call
	ExternalCustomerID string              `form:"external_customer_id" json:"external_customer_id" example:"user_5"`
	CustomerID         string              `form:"customer_id" json:"customer_id" example:"customer456"`
	StartTime          time.Time           `form:"start_time" json:"start_time" example:"2024-11-09T00:00:00Z"`
	EndTime            time.Time           `form:"end_time" json:"end_time" example:"2024-12-09T00:00:00Z"`
	WindowSize         types.WindowSize    `form:"window_size" json:"window_size"`
	Filters            map[string][]string `form:"filters,omitempty" json:"filters,omitempty"`
	PriceID            string              `form:"-" json:"-"` // this is just for internal use to store the price id
}

type GetEventsRequest struct {
	// Customer ID in your system that was sent with the event
	ExternalCustomerID string `json:"external_customer_id"`
	// Event name / Unique identifier for the event in your system
	EventName string `json:"event_name"`
	// Event ID is the idempotency key for the event
	EventID string `json:"event_id"`
	// Start time of the events to be fetched in ISO 8601 format
	// Defaults to last 7 days from now if not provided
	StartTime time.Time `json:"start_time" example:"2024-11-09T00:00:00Z"`
	// End time of the events to be fetched in ISO 8601 format
	// Defaults to now if not provided
	EndTime time.Time `json:"end_time" example:"2024-12-09T00:00:00Z"`
	// First key to iterate over the events
	IterFirstKey string `json:"iter_first_key"`
	// Last key to iterate over the events
	IterLastKey string `json:"iter_last_key"`
	// Property filters to filter the events by the keys in `properties` field of the event
	PropertyFilters map[string][]string `json:"property_filters,omitempty"`
	// Page size to fetch the events and is set to 50 by default
	PageSize int `json:"page_size"`
	// Offset to fetch the events and is set to 0 by default
	Offset int `json:"offset"`
	// Source to filter the events by the source
	Source string `json:"source"`
	// Sort by the field. Allowed values (case sensitive): timestamp, event_name (default: timestamp)
	Sort *string `json:"sort,omitempty" form:"sort" example:"timestamp"`
	// Order by condition. Allowed values (case sensitive): asc, desc (default: desc)
	Order *string `json:"order,omitempty" form:"order" example:"desc"`
	// Count of total number of events
	CountTotal bool `json:"-"`
}

type GetEventsResponse struct {
	Events       []Event `json:"events"`
	HasMore      bool    `json:"has_more"`
	IterFirstKey string  `json:"iter_first_key,omitempty"`
	IterLastKey  string  `json:"iter_last_key,omitempty"`
	TotalCount   uint64  `json:"total_count,omitempty"`
	Offset       int     `json:"offset,omitempty"`
}

type Event struct {
	ID                 string                 `json:"id"`
	ExternalCustomerID string                 `json:"external_customer_id"`
	CustomerID         string                 `json:"customer_id"`
	EventName          string                 `json:"event_name"`
	Timestamp          time.Time              `json:"timestamp"`
	Properties         map[string]interface{} `json:"properties"`
	Source             string                 `json:"source"`
	EnvironmentID      string                 `json:"environment_id"`
}

type GetUsageResponse struct {
	Results   []UsageResult         `json:"results,omitempty"`
	Value     float64               `json:"value,omitempty"`
	EventName string                `json:"event_name"`
	Type      types.AggregationType `json:"type"`
}

type UsageResult struct {
	WindowSize time.Time `json:"window_size"`
	Value      float64   `json:"value"`
}

func FromAggregationResult(result *events.AggregationResult) *GetUsageResponse {
	if result == nil {
		return nil
	}

	response := &GetUsageResponse{
		Results:   make([]UsageResult, len(result.Results)),
		Value:     result.Value.InexactFloat64(),
		EventName: result.EventName,
		Type:      result.Type,
	}

	if len(result.Results) > 0 {
		for i, r := range result.Results {
			response.Results[i] = UsageResult{
				WindowSize: r.WindowSize,
				Value:      r.Value.InexactFloat64(),
			}
		}
	}

	return response
}

func (r *GetUsageRequest) Validate() error {
	return validator.ValidateRequest(r)
}

func (r *GetUsageRequest) ToUsageParams() *events.UsageParams {
	if r.AggregationType == "" || r.PropertyName == "" {
		r.AggregationType = types.AggregationCount
	}

	return &events.UsageParams{
		ExternalCustomerID: r.ExternalCustomerID,
		CustomerID:         r.CustomerID,
		EventName:          r.EventName,
		PropertyName:       r.PropertyName,
		AggregationType:    types.AggregationType(strings.ToUpper(string(r.AggregationType))),
		StartTime:          r.StartTime,
		EndTime:            r.EndTime,
		WindowSize:         r.WindowSize,
		Filters:            r.Filters,
		Multiplier:         r.Multiplier,
	}
}

func (r *GetUsageByMeterRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if err := r.WindowSize.Validate(); err != nil {
		return err
	}

	return nil
}

func (r *GetEventsRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	allowedSortFields := []string{"timestamp", "event_name"}
	if r.Sort != nil && !slices.Contains(allowedSortFields, *r.Sort) {
		return ierr.NewErrorf("invalid sort field: %s", *r.Sort).
			WithHint("Request validation failed due to invalid sort field").
			WithReportableDetails(map[string]any{
				"sort":           *r.Sort,
				"allowed_values": allowedSortFields,
			}).
			Mark(ierr.ErrValidation)
	}

	allowedOrderValues := []string{types.OrderAsc, types.OrderDesc}
	if r.Order != nil && !slices.Contains(allowedOrderValues, *r.Order) {
		return ierr.NewErrorf("invalid order: %s", *r.Order).
			WithHint("Request validation failed due to invalid order by value").
			WithReportableDetails(map[string]any{
				"order":          *r.Order,
				"allowed_values": allowedOrderValues,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

type GetUsageAnalyticsRequest struct {
	ExternalCustomerID string           `json:"external_customer_id" binding:"required"`
	FeatureIDs         []string         `json:"feature_ids,omitempty"`
	Sources            []string         `json:"sources,omitempty"`
	StartTime          time.Time        `json:"start_time,omitempty"`
	EndTime            time.Time        `json:"end_time,omitempty"`
	GroupBy            []string         `json:"group_by,omitempty"` // allowed values: "source", "feature_id"
	WindowSize         types.WindowSize `json:"window_size,omitempty"`
	// Property filters to filter the events by the keys in `properties` field of the event
	PropertyFilters map[string][]string `json:"property_filters,omitempty"`
}

// GetUsageAnalyticsResponse represents the response for the usage analytics API
type GetUsageAnalyticsResponse struct {
	TotalCost decimal.Decimal     `json:"total_cost"`
	Currency  string              `json:"currency"`
	Items     []UsageAnalyticItem `json:"items"`
}

// UsageAnalyticItem represents a single analytic item in the response
type UsageAnalyticItem struct {
	FeatureID       string                `json:"feature_id"`
	FeatureName     string                `json:"name,omitempty"`
	EventName       string                `json:"event_name,omitempty"`
	Source          string                `json:"source,omitempty"`
	Unit            string                `json:"unit,omitempty"`
	UnitPlural      string                `json:"unit_plural,omitempty"`
	AggregationType types.AggregationType `json:"aggregation_type,omitempty"`
	TotalUsage      decimal.Decimal       `json:"total_usage"`
	TotalCost       decimal.Decimal       `json:"total_cost"`
	Currency        string                `json:"currency,omitempty"`
	EventCount      uint64                `json:"event_count"` // Number of events that contributed to this aggregation
	Points          []UsageAnalyticPoint  `json:"points,omitempty"`
}

// UsageAnalyticPoint represents a point in the time series data
type UsageAnalyticPoint struct {
	Timestamp  time.Time       `json:"timestamp"`
	Usage      decimal.Decimal `json:"usage"`
	Cost       decimal.Decimal `json:"cost"`
	EventCount uint64          `json:"event_count"` // Number of events in this time window
}
