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

// AnalyticsBenchmarkRecord is one row in the analytics_benchmark ClickHouse table.
// One row per (feature_id, group_key) per trigger event.
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
	FeatureID   string                        `ch:"feature_id"`
	GroupKey    string                        `ch:"group_key"`
	MatchStatus AnalyticsBenchmarkMatchStatus `ch:"match_status"`

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
