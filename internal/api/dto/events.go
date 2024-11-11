package dto

import "time"

type IngestEventRequest struct {
	ID                 string                 `json:"id" example:"event123"`
	ExternalCustomerID string                 `json:"external_customer_id" validate:"required" binding:"required" example:"customer456"`
	EventName          string                 `json:"event_name" validate:"required" binding:"required" example:"api.request"`
	Timestamp          time.Time              `json:"timestamp" example:"2024-03-20T15:04:05Z"`
	Properties         map[string]interface{} `json:"properties"`
}

type GetUsageRequest struct {
	ExternalCustomerID string    `form:"external_customer_id" binding:"required" example:"customer456"`
	EventName          string    `form:"event_name" binding:"required" example:"api.request"`
	PropertyName       string    `form:"property_name" example:"request.size"`
	AggregationType    string    `form:"aggregation_type" example:"sum"`
	StartTime          time.Time `form:"start_time" example:"2024-03-13T00:00:00Z"`
	EndTime            time.Time `form:"end_time" example:"2024-03-20T00:00:00Z"`
}
