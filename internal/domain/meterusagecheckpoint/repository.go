package meterusagecheckpoint

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Key identifies one customer's running aggregate for a meter.
type Key struct {
	CustomerID      string
	MeterID         string
	AggregationType types.AggregationType
}

// Repository stores running aggregates, one per customer, meter and instant.
type Repository interface {
	// GetLatestAtOrBefore returns the newest checkpoint for key with aggregated_until <= at,
	// or nil when there is none. Checkpoints of another aggregation type are not returned.
	GetLatestAtOrBefore(ctx context.Context, key Key, at time.Time) (*MeterUsageCheckpoint, error)

	// Upsert writes the checkpoint. An existing one for the same customer, meter and
	// aggregated_until keeps its id and takes the new total and aggregation type, so
	// recomputing a period is idempotent.
	Upsert(ctx context.Context, c *MeterUsageCheckpoint) (*MeterUsageCheckpoint, error)
}
