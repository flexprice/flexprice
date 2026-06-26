package events

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// AnalyticsBenchmarkRecord is one row in the analytics_benchmark ClickHouse table.
// One row per benchmark trigger event: the consumer runs the captured analytics
// request against meter_usage twice (no-FINAL and FINAL) and writes both sides'
// wall-clock duration, server-side query counters, and result totals so a SQL
// diff is one row away.
type AnalyticsBenchmarkRecord struct {
	TenantID      string    `ch:"tenant_id"`
	EnvironmentID string    `ch:"environment_id"`
	EventID       string    `ch:"event_id"`
	StartTime     time.Time `ch:"start_time"`
	EndTime       time.Time `ch:"end_time"`

	// Request fields (verbatim from the captured analytics request).
	ExternalCustomerID  string   `ch:"external_customer_id"`
	ExternalCustomerIDs []string `ch:"external_customer_ids"`
	FeatureIDs          []string `ch:"feature_ids"`
	Sources             []string `ch:"sources"`
	GroupBy             []string `ch:"group_by"`
	WindowSize          string   `ch:"window_size"`
	Expand              []string `ch:"expand"`
	IncludeChildren     uint8    `ch:"include_children"`
	HasPropertyFilters  uint8    `ch:"has_property_filters"`
	RequestJSON         string   `ch:"request_json"`

	// No-FINAL perf (server-side counters aggregated across main + N points sub-queries).
	NoFinalDurationMs    float64 `ch:"nofinal_duration_ms"`
	NoFinalScanRows      uint64  `ch:"nofinal_scan_rows"`
	NoFinalScanBytes     uint64  `ch:"nofinal_scan_bytes"`
	NoFinalReadDiskBytes uint64  `ch:"nofinal_read_disk_bytes"`
	NoFinalMemPeakBytes  uint64  `ch:"nofinal_mem_peak_bytes"`
	NoFinalResultRows    uint64  `ch:"nofinal_result_rows"`

	// FINAL perf (same shape).
	FinalDurationMs    float64 `ch:"final_duration_ms"`
	FinalScanRows      uint64  `ch:"final_scan_rows"`
	FinalScanBytes     uint64  `ch:"final_scan_bytes"`
	FinalReadDiskBytes uint64  `ch:"final_read_disk_bytes"`
	FinalMemPeakBytes  uint64  `ch:"final_mem_peak_bytes"`
	FinalResultRows    uint64  `ch:"final_result_rows"`

	// Pre-computed signed diffs (final - nofinal). FINAL can be lighter on small
	// ranges (fewer parts to merge), so Int64 not UInt64.
	DurationDiffMs    float64 `ch:"duration_diff_ms"`
	ScanRowsDiff      int64   `ch:"scan_rows_diff"`
	ScanBytesDiff     int64   `ch:"scan_bytes_diff"`
	ReadDiskBytesDiff int64   `ch:"read_disk_bytes_diff"`
	MemPeakDiffBytes  int64   `ch:"mem_peak_diff_bytes"`

	// Per-side totals + diffs. Totals come from response.TotalCost and
	// sum(items[i].TotalUsage). Sign: nofinal - final (matches the historical
	// shape so downstream queries keep working).
	NoFinalTotalUsage decimal.Decimal `ch:"nofinal_total_usage"`
	FinalTotalUsage   decimal.Decimal `ch:"final_total_usage"`
	UsageDiff         decimal.Decimal `ch:"usage_diff"`
	NoFinalTotalCost  decimal.Decimal `ch:"nofinal_total_cost"`
	FinalTotalCost    decimal.Decimal `ch:"final_total_cost"`
	CostDiff          decimal.Decimal `ch:"cost_diff"`
	NoFinalItemCount  uint64          `ch:"nofinal_item_count"`
	FinalItemCount    uint64          `ch:"final_item_count"`
	ResultsMatch      uint8           `ch:"results_match"`

	NoFinalError string `ch:"nofinal_error"`
	FinalError   string `ch:"final_error"`

	// "nofinal" or "final" — which side ran first this round. Lets us detect
	// or filter cache-warmth bias post-hoc.
	FirstSide string    `ch:"first_side"`
	Currency  string    `ch:"currency"`
	CreatedAt time.Time `ch:"created_at"`
}

// AnalyticsBenchmarkRepository persists analytics benchmark comparison rows.
type AnalyticsBenchmarkRepository interface {
	// BulkInsert writes all rows in one batched statement. Callers typically
	// pass a single-element slice (one row per benchmark trigger event) but the
	// batched signature is preserved for future flexibility.
	BulkInsert(ctx context.Context, records []*AnalyticsBenchmarkRecord) error
}
