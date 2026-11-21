package meterusagecheckpoint

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// GetLatestBeforeParams selects one customer's checkpoints for a meter and aggregation type.
type GetLatestBeforeParams struct {
	CustomerID      string
	MeterID         string
	AggregationType types.AggregationType
	Before          time.Time
}

// PeriodCheckpoint is how far one customer's per-period totals are aggregated.
type PeriodCheckpoint struct {
	CustomerID      string
	PeriodStart     time.Time
	AggregatedUntil time.Time
}

// PeriodTotal is one meter's usage to add to a period total.
type PeriodTotal struct {
	MeterID         string
	AggregationType types.AggregationType
	Quantity        decimal.Decimal
}

// PeriodAdvance adds Totals to one customer's period and moves its checkpoint from From to To.
type PeriodAdvance struct {
	CustomerID  string
	PeriodStart time.Time
	// From is the aggregated_until the run read: the period start for a period with no checkpoint yet.
	From   time.Time
	To     time.Time
	Totals []PeriodTotal
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

	// ListPeriodCheckpoints returns the period checkpoints of the given customers.
	ListPeriodCheckpoints(ctx context.Context, customerIDs []string) ([]*PeriodCheckpoint, error)

	// AdvancePeriods applies every advance in one transaction: SUM and COUNT totals add, MAX keeps the
	// higher value. An advance whose checkpoint is no longer at From is skipped, so a retry or an
	// overlapping run never adds usage twice. It returns the number of advances applied.
	AdvancePeriods(ctx context.Context, advances []*PeriodAdvance) (int, error)
}
