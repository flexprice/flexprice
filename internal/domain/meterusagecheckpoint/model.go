package meterusagecheckpoint

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// MeterUsageCheckpoint is a meter's running aggregate for one customer: every change
// before AggregatedUntil, aggregated by AggregationType.
type MeterUsageCheckpoint struct {
	ID              string                `json:"id"`
	CustomerID      string                `json:"customer_id"`
	MeterID         string                `json:"meter_id"`
	AggregationType types.AggregationType `json:"aggregation_type"`
	Total           decimal.Decimal       `json:"total"`
	AggregatedUntil time.Time             `json:"aggregated_until"`
	InvoiceID       *string               `json:"invoice_id,omitempty"`
	EnvironmentID   string                `json:"environment_id"`
	types.BaseModel
}

func FromEnt(e *ent.MeterUsageCheckpoint) *MeterUsageCheckpoint {
	if e == nil {
		return nil
	}
	return &MeterUsageCheckpoint{
		ID:              e.ID,
		CustomerID:      e.CustomerID,
		MeterID:         e.MeterID,
		AggregationType: e.AggregationType,
		Total:           e.Total,
		AggregatedUntil: e.AggregatedUntil,
		InvoiceID:       e.InvoiceID,
		EnvironmentID:   e.EnvironmentID,
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
