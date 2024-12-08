package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

type IngestEventRequest struct {
	EventName          string                 `json:"event_name" validate:"required" binding:"required" example:"api_request"`
	EventID            string                 `json:"event_id" example:"event123"`
	CustomerID         string                 `json:"customer_id" example:"customer456"`
	ExternalCustomerID string                 `json:"external_customer_id" validate:"required" binding:"required" example:"customer456"`
	Timestamp          time.Time              `json:"timestamp" example:"2024-03-20T15:04:05Z"`
	Source             string                 `json:"source" example:"api"`
	Properties         map[string]interface{} `json:"properties" swaggertype:"object,string,number" example:"{\"request_size\":100,\"response_status\":200}"`
}

type GetUsageRequest struct {
	ExternalCustomerID string              `form:"external_customer_id" example:"customer456"`
	EventName          string              `form:"event_name" binding:"required" example:"api_request"`
	PropertyName       string              `form:"property_name" example:"request_size"` // will be empty/ignored in case of COUNT
	AggregationType    string              `form:"aggregation_type" binding:"required" example:"COUNT"`
	StartTime          time.Time           `form:"start_time" example:"2024-03-13T00:00:00Z"`
	EndTime            time.Time           `form:"end_time" example:"2024-03-20T00:00:00Z"`
	WindowSize         types.WindowSize    `form:"window_size" example:"HOUR"`
	Filters            map[string][]string `form:"filters,omitempty"`
}

type GetUsageByMeterRequest struct {
	MeterID            string           `form:"meter_id" binding:"required" example:"123"`
	ExternalCustomerID string           `form:"external_customer_id" example:"user_5"`
	StartTime          time.Time        `form:"start_time" example:"2024-11-09T00:00:00Z"`
	EndTime            time.Time        `form:"end_time" example:"2024-12-09T00:00:00Z"`
	WindowSize         types.WindowSize `form:"window_size" example:"HOUR"`
}

type GetEventsRequest struct {
	ExternalCustomerID string    `json:"external_customer_id"`
	EventName          string    `json:"event_name" binding:"required"`
	StartTime          time.Time `json:"start_time" example:"2024-11-09T00:00:00Z"`
	EndTime            time.Time `json:"end_time" example:"2024-12-09T00:00:00Z"`
	IterFirstKey       string    `json:"iter_first_key"`
	IterLastKey        string    `json:"iter_last_key"`
	PageSize           int       `json:"page_size" default:"50"`
}

type GetEventsResponse struct {
	Events       []Event `json:"events"`
	HasMore      bool    `json:"has_more"`
	IterFirstKey string  `json:"iter_first_key,omitempty"`
	IterLastKey  string  `json:"iter_last_key,omitempty"`
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

func (r *IngestEventRequest) Validate() error {
	return validator.New().Struct(r)
}

func (r *GetUsageRequest) Validate() error {
	return validator.New().Struct(r)
}

func (r *GetUsageByMeterRequest) Validate() error {
	return validator.New().Struct(r)
}

func (r *GetEventsRequest) Validate() error {
	return validator.New().Struct(r)
}
