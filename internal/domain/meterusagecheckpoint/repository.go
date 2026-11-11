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

// Repository stores running aggregates, one per customer, meter and instant.
type Repository interface {
	// GetLatestBefore returns the newest checkpoint with aggregated_until < params.Before,
	// or nil when there is none. Checkpoints of another aggregation type are not returned.
	GetLatestBefore(ctx context.Context, params GetLatestBeforeParams) (*MeterUsageCheckpoint, error)

	// Upsert writes the checkpoint. An existing one for the same customer, meter and
	// aggregated_until keeps its id and takes the new total and aggregation type, so
	// recomputing a period is idempotent.
	Upsert(ctx context.Context, c *MeterUsageCheckpoint) (*MeterUsageCheckpoint, error)
}
