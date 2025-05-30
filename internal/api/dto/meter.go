package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

// CreateMeterRequest represents the request payload for creating a meter
type CreateMeterRequest struct {
	Name        string            `json:"name" binding:"required" example:"API Usage Meter"`
	EventName   string            `json:"event_name" binding:"required" example:"api_request"`
	Aggregation meter.Aggregation `json:"aggregation" binding:"required"`
	Filters     []meter.Filter    `json:"filters"`
	ResetUsage  types.ResetUsage  `json:"reset_usage" binding:"required"`
}

// UpdateMeterRequest represents the request payload for updating a meter
type UpdateMeterRequest struct {
	Filters []meter.Filter `json:"filters"`
}

// MeterResponse represents the meter response structure
type MeterResponse struct {
	ID          string            `json:"id" example:"550e8400-e29b-41d4-a716-446655440000"`
	Name        string            `json:"name" example:"API Usage Meter"`
	TenantID    string            `json:"tenant_id" example:"tenant123"`
	EventName   string            `json:"event_name" example:"api_request"`
	Aggregation meter.Aggregation `json:"aggregation"`
	Filters     []meter.Filter    `json:"filters"`
	ResetUsage  types.ResetUsage  `json:"reset_usage"`
	CreatedAt   time.Time         `json:"created_at" example:"2024-03-20T15:04:05Z"`
	UpdatedAt   time.Time         `json:"updated_at" example:"2024-03-20T15:04:05Z"`
	Status      string            `json:"status" example:"published"`
}

func (r *MeterResponse) ToMeter() *meter.Meter {
	return &meter.Meter{
		ID:          r.ID,
		Name:        r.Name,
		EventName:   r.EventName,
		Aggregation: r.Aggregation,
		Filters:     r.Filters,
		ResetUsage:  r.ResetUsage,
		BaseModel: types.BaseModel{
			Status:    types.Status(r.Status),
			CreatedAt: r.CreatedAt,
			UpdatedAt: r.UpdatedAt,
			TenantID:  r.TenantID,
		},
	}
}

// Convert domain Meter to MeterResponse
func ToMeterResponse(m *meter.Meter) *MeterResponse {
	return &MeterResponse{
		ID:          m.ID,
		Name:        m.Name,
		TenantID:    m.TenantID,
		EventName:   m.EventName,
		Aggregation: m.Aggregation,
		Filters:     m.Filters,
		ResetUsage:  m.ResetUsage,
		CreatedAt:   m.CreatedAt,
		UpdatedAt:   m.UpdatedAt,
		Status:      string(m.Status),
	}
}

// Convert CreateMeterRequest to domain Meter
func (r *CreateMeterRequest) ToMeter(tenantID, createdBy string) *meter.Meter {
	m := meter.NewMeter(r.Name, tenantID, createdBy)
	m.EventName = r.EventName
	m.Aggregation = r.Aggregation
	m.Filters = r.Filters
	m.ResetUsage = r.ResetUsage
	m.Status = types.StatusPublished
	return m
}

// Request validations
func (r *CreateMeterRequest) Validate() error {
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	if err := r.ResetUsage.Validate(); err != nil {
		return err
	}

	return nil
}

// ListMetersResponse represents a paginated list of meters
type ListMetersResponse = types.ListResponse[*MeterResponse]
