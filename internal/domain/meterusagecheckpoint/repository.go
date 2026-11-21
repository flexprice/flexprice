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

// UsageWatermark is how far a customer's usage has been added to their period totals.
type UsageWatermark struct {
	CustomerID      string
	AggregatedUntil time.Time
}

// PeriodTotal is one meter's usage to add to one billing period's total.
type PeriodTotal struct {
	PeriodStart     time.Time
	MeterID         string
	AggregationType types.AggregationType
	Quantity        decimal.Decimal
}

// CustomerCheckpointUpdate adds Totals to a customer's period totals and moves their watermark from From to To.
type CustomerCheckpointUpdate struct {
	CustomerID string
	// From is the watermark the run read; zero for a customer with none.
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

	// ListUsageWatermarks returns the watermarks of the given customers.
	ListUsageWatermarks(ctx context.Context, customerIDs []string) ([]*UsageWatermark, error)

	// UpdateCustomerCheckpoints applies every update in one transaction: SUM and COUNT totals add, MAX
	// keeps the higher value. An update whose watermark is no longer at From is skipped, so a retry or
	// an overlapping run never adds usage twice. It returns the number of updates applied.
	UpdateCustomerCheckpoints(ctx context.Context, updates []*CustomerCheckpointUpdate) (int, error)
}
