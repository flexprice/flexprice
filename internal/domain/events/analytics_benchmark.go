package events

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// AnalyticsBenchmarkMatchStatus is the join-side discriminator for analytics benchmark rows.
type AnalyticsBenchmarkMatchStatus string

const (
	AnalyticsBenchmarkMatchMatched     AnalyticsBenchmarkMatchStatus = "matched"
	AnalyticsBenchmarkMatchFeatureOnly AnalyticsBenchmarkMatchStatus = "feature_only"
	AnalyticsBenchmarkMatchMeterOnly   AnalyticsBenchmarkMatchStatus = "meter_only"
)

// AnalyticsBenchmarkRowType tiers benchmark rows by granularity so debugging can
// drill from "did billing match?" down to "where exactly did it differ?".
type AnalyticsBenchmarkRowType string

const (
	// AnalyticsBenchmarkRowSummary: 1 row per event_id with response.TotalCost
	// from each pipeline — the authoritative "did billing match?" signal.
	AnalyticsBenchmarkRowSummary AnalyticsBenchmarkRowType = "summary"
	// AnalyticsBenchmarkRowFeature: 1 row per (event_id, feature_id, group_key)
	// with items aggregated across line-item splits — robust to duplicate
	// feature_ids and order-independent.
	AnalyticsBenchmarkRowFeature AnalyticsBenchmarkRowType = "feature"
	// AnalyticsBenchmarkRowLineItem: 1 row per
	// (event_id, feature_id, sub_line_item_id, group_key) — granular drill-down.
	AnalyticsBenchmarkRowLineItem AnalyticsBenchmarkRowType = "line_item"
)

// AnalyticsBenchmarkDiffReason classifies the cost_diff on each row so spurious
// mismatches (data-shape edge cases that don't affect billing) can be filtered
// out from real bugs.
type AnalyticsBenchmarkDiffReason string

const (
	// AnalyticsBenchmarkDiffNone: cost_diff is exactly zero.
	AnalyticsBenchmarkDiffNone AnalyticsBenchmarkDiffReason = "none"
	// AnalyticsBenchmarkDiffUnmatched: one side produced no item for this key.
	AnalyticsBenchmarkDiffUnmatched AnalyticsBenchmarkDiffReason = "unmatched"
	// AnalyticsBenchmarkDiffMultiFeatureMeter: the feature is one of multiple
	// features mapped to the same meter. The meter pipeline's runtime
	// meter→feature lookup is 1:1 and can attribute to a different feature than
	// the ingest-time snapshot in feature_usage — diff is expected, not a bug.
	AnalyticsBenchmarkDiffMultiFeatureMeter AnalyticsBenchmarkDiffReason = "multi_feature_meter"
	// AnalyticsBenchmarkDiffMultiItem: multiple items collapsed at this row's
	// granularity on one side (look at a finer row_type for the real story).
	AnalyticsBenchmarkDiffMultiItem AnalyticsBenchmarkDiffReason = "multi_item"
	// AnalyticsBenchmarkDiffMaterial: non-zero cost_diff with no above
	// explanation — this is a real bug.
	AnalyticsBenchmarkDiffMaterial AnalyticsBenchmarkDiffReason = "material"
)

// AnalyticsBenchmarkRecord is one row in the analytics_benchmark ClickHouse table.
// Multiple rows per event_id at different RowType granularities.
type AnalyticsBenchmarkRecord struct {
	TenantID      string    `ch:"tenant_id"`
	EnvironmentID string    `ch:"environment_id"`
	EventID       string    `ch:"event_id"`
	StartTime     time.Time `ch:"start_time"`
	EndTime       time.Time `ch:"end_time"`

	// Parsed request fields
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

	// Per-item row
	RowType       AnalyticsBenchmarkRowType     `ch:"row_type"`
	FeatureID     string                        `ch:"feature_id"`
	MeterID       string                        `ch:"meter_id"`
	SubLineItemID string                        `ch:"sub_line_item_id"`
	GroupKey      string                        `ch:"group_key"`
	MatchStatus   AnalyticsBenchmarkMatchStatus `ch:"match_status"`
	DiffReason    AnalyticsBenchmarkDiffReason  `ch:"diff_reason"`

	// Price ids used by each pipeline (empty when that side did not match).
	FeaturePriceID string `ch:"feature_price_id"`
	MeterPriceID   string `ch:"meter_price_id"`

	// How many response items contributed to each side of this row.
	FeatureItemCount uint64 `ch:"feature_item_count"`
	MeterItemCount   uint64 `ch:"meter_item_count"`

	FeatureTotalUsage decimal.Decimal `ch:"feature_total_usage"`
	MeterTotalUsage   decimal.Decimal `ch:"meter_total_usage"`
	UsageDiff         decimal.Decimal `ch:"usage_diff"`

	FeatureTotalCost decimal.Decimal `ch:"feature_total_cost"`
	MeterTotalCost   decimal.Decimal `ch:"meter_total_cost"`
	CostDiff         decimal.Decimal `ch:"cost_diff"`

	FeatureEventCount uint64 `ch:"feature_event_count"`
	MeterEventCount   uint64 `ch:"meter_event_count"`

	Currency  string    `ch:"currency"`
	CreatedAt time.Time `ch:"created_at"`
}

// AnalyticsBenchmarkRepository persists analytics benchmark comparison rows.
type AnalyticsBenchmarkRepository interface {
	// BulkInsert writes all rows from a single trigger event in one batched statement.
	BulkInsert(ctx context.Context, records []*AnalyticsBenchmarkRecord) error
}
