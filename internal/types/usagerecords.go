package types

import "time"

// UsageRecordSyncEntry records the outcome of reporting a usage record to one destination (today,
// always a marketplace connection). Stored in usage_records.syncs, keyed by connection_id.
// Marketplace is carried on the entry itself so it reads back self-describing even if the
// connection is later deleted.
//
// An entry is one of two kinds, distinguished by Skipped:
//   - A real acceptance: Skipped is false, ReportingID carries the marketplace's receipt.
//   - A skip: Skipped is true, ReportingID is empty, SkipReason explains why. Used only when
//     sending is deterministically pointless for this provider (e.g. a zero-amount row on Azure,
//     which documents a zero quantity as invalid). This still resolves the connection so the row's
//     synced flag can reach true, without claiming anything was posted.
type UsageRecordSyncEntry struct {
	Marketplace SecretProvider `json:"marketplace"`
	ReportingID string         `json:"reporting_id"`
	SyncedAt    time.Time      `json:"synced_at"`
	Skipped     bool           `json:"skipped,omitempty"`
	SkipReason  string         `json:"skip_reason,omitempty"`
}
