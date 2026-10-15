package meterusagecheckpoint

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// MeterUsageCheckpoint is a SIGNED_SUM meter's running total for one customer as of AsOf.
type MeterUsageCheckpoint struct {
	ID            string          `json:"id"`
	CustomerID    string          `json:"customer_id"`
	MeterID       string          `json:"meter_id"`
	Total         decimal.Decimal `json:"total"`
	AsOf          time.Time       `json:"as_of"`
	InvoiceID     *string         `json:"invoice_id,omitempty"`
	EnvironmentID string          `json:"environment_id"`
	types.BaseModel
}

func FromEnt(e *ent.MeterUsageCheckpoint) *MeterUsageCheckpoint {
	if e == nil {
		return nil
	}
	return &MeterUsageCheckpoint{
		ID:            e.ID,
		CustomerID:    e.CustomerID,
		MeterID:       e.MeterID,
		Total:         e.Total,
		AsOf:          e.AsOf,
		InvoiceID:     e.InvoiceID,
		EnvironmentID: e.EnvironmentID,
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
