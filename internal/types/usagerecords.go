package types

import "time"

// UsageRecordSyncEntry records the outcome of reporting a usage record to one destination (today,
// always a marketplace connection). Stored in usage_records.syncs, keyed by connection_id.
// Marketplace is carried on the entry itself so it reads back self-describing even if the
// connection is later deleted.
type UsageRecordSyncEntry struct {
	Marketplace SecretProvider `json:"marketplace"`
	ReportingID string         `json:"reporting_id"`
	SyncedAt    time.Time      `json:"synced_at"`
}
