package meterusage

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/shopspring/decimal"
)

// MeterUsage represents a single event-meter enriched row in the meter_usage ClickHouse table.
// One raw event can produce multiple MeterUsage rows — one per matching meter.
// ID is overridden to a composite key (eventID + "_" + meterID) so that each
// event-meter pair occupies a distinct row under ReplacingMergeTree deduplication.
type MeterUsage struct {
	events.Event
	MeterID    string          `json:"meter_id" ch:"meter_id" validate:"required"`
	QtyTotal   decimal.Decimal `json:"qty_total" ch:"qty_total" swaggertype:"string"`
	UniqueHash string          `json:"unique_hash" ch:"unique_hash" validate:"required"` // property value for COUNT_UNIQUE; "" otherwise
}

// Repository is the write interface for meter_usage rows.
type Repository interface {
	BulkInsert(ctx context.Context, records []*MeterUsage) error
}
