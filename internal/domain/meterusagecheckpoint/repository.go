package meterusagecheckpoint

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// GetLatestBeforeParams selects one customer's checkpoints for a meter and aggregation type.
type GetLatestBeforeParams struct {
	CustomerID      string
	MeterID         string
	AggregationType types.AggregationType
	Before          time.Time
}

// UpdatePeriodTotalsParams moves one customer's per-period rows for one billing period
// from ExpectedAggregatedUntil to the AggregatedUntil set on each row.
type UpdatePeriodTotalsParams struct {
	CustomerID  string
	PeriodStart time.Time
	// ExpectedAggregatedUntil is the aggregated_until every stored row of the period must still have.
	ExpectedAggregatedUntil time.Time
	// Rows carry the new total and aggregated_until; a row missing from the table is created.
	Rows []*MeterUsageCheckpoint
}

// Repository stores running totals, one per customer, meter and instant, and per-period
// totals, one per customer, meter and billing period.
type Repository interface {
	// GetLatestBefore returns the newest running total with aggregated_until < params.Before,
	// or nil when there is none. Checkpoints of another aggregation type are not returned.
	GetLatestBefore(ctx context.Context, params GetLatestBeforeParams) (*MeterUsageCheckpoint, error)

	// Upsert writes the checkpoint. An existing one for the same customer, meter and
	// aggregated_until keeps its id and takes the new total and aggregation type, so
	// recomputing a period is idempotent.
	Upsert(ctx context.Context, c *MeterUsageCheckpoint) (*MeterUsageCheckpoint, error)

	// ListPeriodTotals returns the customer's per-period rows for one billing period.
	ListPeriodTotals(ctx context.Context, customerID string, periodStart time.Time) ([]*MeterUsageCheckpoint, error)

	// UpdatePeriodTotals writes every row in one transaction, or nothing and a version-conflict
	// error when any stored row of the period is no longer at params.ExpectedAggregatedUntil.
	UpdatePeriodTotals(ctx context.Context, params UpdatePeriodTotalsParams) error
}
