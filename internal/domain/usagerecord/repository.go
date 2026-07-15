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

	// ListUnsynced returns the tenant/environment's usage records that are not yet fully synced.
	ListUnsynced(ctx context.Context, tenantID, environmentID string) ([]*UsageRecord, error)

	// UpdateSyncResult records a marketplace's sync outcome on a usage record and sets whether the
	// record is now synced to every marketplace.
	UpdateSyncResult(ctx context.Context, id string, marketplace Marketplace, entry MarketplaceSyncEntry, allProvidersSynced bool) error
}
