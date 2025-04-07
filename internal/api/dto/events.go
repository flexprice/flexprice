package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
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
}

type GetUsageByMeterRequest struct {
	MeterID            string              `form:"meter_id" json:"meter_id" binding:"required" example:"123"`
	ExternalCustomerID string              `form:"external_customer_id" json:"external_customer_id" example:"user_5"`
	CustomerID         string              `form:"customer_id" json:"customer_id" example:"customer456"`
	StartTime          time.Time           `form:"start_time" json:"start_time" example:"2024-11-09T00:00:00Z"`
	EndTime            time.Time           `form:"end_time" json:"end_time" example:"2024-12-09T00:00:00Z"`
	WindowSize         types.WindowSize    `form:"window_size" json:"window_size"`
	Filters            map[string][]string `form:"filters,omitempty" json:"filters,omitempty"`
}

type GetEventsRequest struct {
	ExternalCustomerID string              `json:"external_customer_id"`
	EventName          string              `json:"event_name" binding:"required"`
	EventID            string              `json:"event_id"`
	StartTime          time.Time           `json:"start_time" example:"2024-11-09T00:00:00Z"`
	EndTime            time.Time           `json:"end_time" example:"2024-12-09T00:00:00Z"`
	IterFirstKey       string              `json:"iter_first_key"`
	IterLastKey        string              `json:"iter_last_key"`
	PropertyFilters    map[string][]string `json:"property_filters,omitempty"`
	PageSize           int                 `json:"page_size"`
	Offset             int                 `json:"offset"`
	CountTotal         bool                `json:"count_total"`
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
	return validator.ValidateRequest(r)
}
