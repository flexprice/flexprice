package meterusagecheckpoint

import (
	"context"
	"time"
)

// Repository stores SIGNED_SUM running totals, one per customer, meter and instant.
type Repository interface {
	// GetLatestAtOrBefore returns the newest checkpoint with as_of <= at, or nil when there is none.
	GetLatestAtOrBefore(ctx context.Context, customerID, meterID string, at time.Time) (*MeterUsageCheckpoint, error)

	// Upsert writes the checkpoint. An existing one for the same customer, meter and
	// as_of keeps its id and takes the new total, so recomputing a period is idempotent.
	Upsert(ctx context.Context, c *MeterUsageCheckpoint) (*MeterUsageCheckpoint, error)
}
