package usagerecord

import (
	"context"
	"time"
)

// Repository provides persistence for usage records.
type Repository interface {
	// Create inserts a new usage record.
	Create(ctx context.Context, record *UsageRecord) error

	// ExistsForPeriod reports whether a usage record already exists for this subscription and
	// exact reporting window. Used to make the snapshot activity idempotent under Temporal
	// retries: the window is deterministic per scheduled run, so a matching row means this
	// subscription was already processed by an earlier attempt.
	ExistsForPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time) (bool, error)

	// ListUnsyncedByConnection returns one connection's usage records that have not yet been reported.
	ListUnsyncedByConnection(ctx context.Context, tenantID, environmentID, connectionID string) ([]*UsageRecord, error)

	// MarkSynced records that a usage record was successfully reported: sets synced=true, stamps
	// synced_at, and stores the marketplace's report identifier (AWS's MeteringRecordId, or GCP's
	// operationId — which is always the record's own id, since GCP's services.report returns no
	// per-record receipt of its own).
	MarkSynced(ctx context.Context, id string, marketplaceReportID string) error
}
