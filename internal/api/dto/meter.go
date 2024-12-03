package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

// CreateMeterRequest represents the request payload for creating a meter
type CreateMeterRequest struct {
	EventName   string            `json:"event_name" binding:"required" example:"api_request"`
	Aggregation meter.Aggregation `json:"aggregation" binding:"required"`
}

// MeterResponse represents the meter response structure
type MeterResponse struct {
	ID          string            `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	TenantID    string            `json:"tenant_id" example:"tenant123"`
	EventName   string            `json:"event_name" example:"api_request"`
	Aggregation meter.Aggregation `json:"aggregation"`
	CreatedAt   time.Time         `json:"created_at" example:"2024-03-20T15:04:05Z"`
	UpdatedAt   time.Time         `json:"updated_at" example:"2024-03-20T15:04:05Z"`
	Status      string            `json:"status" example:"ACTIVE"`
}

type SyncStripeUsageRequest struct {
	MeterID                  string    `json:"meter_id" binding:"required" example:"d62e8435-ecf2-43cc-9f9f-5589a736ccf7"`
	ExternalCustomerID       string    `json:"external_customer_id" binding:"required" example:"user_5"`
	StartTime                time.Time `json:"start_time" binding:"required" example:"2024-11-09T00:00:00Z"`
	EndTime                  time.Time `json:"end_time" binding:"required" example:"2024-12-09T00:00:00Z"`
	StripeSubscriptionItemID string    `json:"stripe_subscription_item_id" binding:"required" example:"sub_item_123"`
}

type SyncStripeUsageResponse struct {
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
}

// Convert domain Meter to MeterResponse
func ToMeterResponse(m *meter.Meter) *MeterResponse {
	return &MeterResponse{
		ID:          m.ID,
		TenantID:    m.TenantID,
		EventName:   m.EventName,
		Aggregation: m.Aggregation,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		Status:      string(m.Status),
	}
}

// Convert CreateMeterRequest to domain Meter
func (r *CreateMeterRequest) ToMeter(tenantID, createdBy string) *meter.Meter {
	m := meter.NewMeter("", tenantID, createdBy)
	m.EventName = r.EventName
	m.Aggregation = r.Aggregation
	m.Status = types.StatusActive
	return m
}

// Request validations
func (r *CreateMeterRequest) Validate() error {
	return validator.New().Struct(r)
}
