package usagerecord

import (
	"context"
)

// Repository provides persistence for usage records.
type Repository interface {
	// Create inserts a new usage record.
	Create(ctx context.Context, record *UsageRecord) error

	// ListUnsynced returns the tenant/environment's usage records that are not yet fully synced.
	ListUnsynced(ctx context.Context, tenantID, environmentID string) ([]*UsageRecord, error)

	// UpdateSyncResult records a marketplace's sync outcome on a usage record and sets whether the
	// record is now synced to every marketplace.
	UpdateSyncResult(ctx context.Context, id string, marketplace Marketplace, entry MarketplaceSyncEntry, allProvidersSynced bool) error
}
