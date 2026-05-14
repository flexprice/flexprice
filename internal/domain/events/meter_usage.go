package events

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// MeterUsage represents a meter-level usage record in the meter_usage ClickHouse table.
// It embeds Event for shared fields and adds meter-specific columns:
// meter_id, qty_total, unique_hash. ingested_at is handled by ClickHouse DEFAULT.
type MeterUsage struct {
	Event

	// MeterID is the matched meter for this event
	MeterID string `json:"meter_id" ch:"meter_id"`

	// QtyTotal is the extracted quantity based on meter aggregation config
	QtyTotal decimal.Decimal `json:"qty_total" ch:"qty_total" swaggertype:"string"`

	// UniqueHash is the dedup hash (populated for COUNT_UNIQUE, event_name:event_id otherwise)
	UniqueHash string `json:"unique_hash" ch:"unique_hash"`
}

// MeterUsageQueryParams defines filters for querying the meter_usage table
type MeterUsageQueryParams struct {
	TenantID           string
	EnvironmentID      string
	ExternalCustomerID string
	// ExternalCustomerIDs supports multi-customer queries (e.g. inherited subscriptions)
	ExternalCustomerIDs []string
	MeterID             string
	MeterIDs            []string
	StartTime          time.Time
	EndTime            time.Time
	AggregationType    types.AggregationType
	WindowSize         types.WindowSize
	BillingAnchor      *time.Time
	// GroupByProperty is the JSON property key for group-by aggregation (e.g. for bucketed MAX meters)
	GroupByProperty string
	// UseFinal enables FINAL for ReplacingMergeTree deduplication (use for billing queries)
	UseFinal bool
}

// MeterUsageResult represents a single time-bucketed aggregation point
type MeterUsageResult struct {
	WindowStart time.Time       `json:"window_start"`
	Value       decimal.Decimal `json:"value"`
	EventCount  uint64          `json:"event_count"`
}

// MeterUsageAggregationResult holds the total aggregated value and optional time-series breakdown
type MeterUsageAggregationResult struct {
	MeterID         string                `json:"meter_id"`
	AggregationType types.AggregationType `json:"aggregation_type"`
	TotalValue      decimal.Decimal       `json:"total_value"`
	EventCount      uint64                `json:"event_count"`
	Points          []MeterUsageResult    `json:"points,omitempty"`
}

// MeterUsageDetailedAnalyticsParams defines parameters for detailed meter usage analytics
// with support for group by, property filters, source filtering, and time-series breakdown.
type MeterUsageDetailedAnalyticsParams struct {
	TenantID            string
	EnvironmentID       string
	ExternalCustomerID  string
	ExternalCustomerIDs []string
	MeterIDs            []string
	StartTime           time.Time
	EndTime             time.Time
	GroupBy             []string            // "source", "meter_id", "properties.<field>"
	PropertyFilters     map[string][]string // e.g. {"model": ["gpt-4", "gpt-3.5"]}
	Sources             []string
	AggregationTypes    []types.AggregationType // SUM, MAX, LATEST, COUNT_UNIQUE, COUNT
	WindowSize          types.WindowSize
	BillingAnchor       *time.Time
	UseFinal            bool
}

// MeterUsageDetailedResult holds aggregated analytics for a single group combination
type MeterUsageDetailedResult struct {
	MeterID          string
	Source           string
	Sources          []string          // populated when source is NOT in group_by
	Properties       map[string]string // property group-by values
	TotalUsage       decimal.Decimal
	MaxUsage         decimal.Decimal
	LatestUsage      decimal.Decimal
	CountUniqueUsage uint64
	EventCount       uint64
	Points           []MeterUsageDetailedPoint
}

// MeterUsageDetailedPoint is a single time-bucketed data point with all aggregation values
type MeterUsageDetailedPoint struct {
	WindowStart      time.Time
	TotalUsage       decimal.Decimal
	MaxUsage         decimal.Decimal
	LatestUsage      decimal.Decimal
	CountUniqueUsage uint64
	EventCount       uint64
}

// MeterUsageRepository defines read/write operations on the meter_usage ClickHouse table
type MeterUsageRepository interface {
	// BulkInsertMeterUsage inserts multiple meter usage records in batches
	BulkInsertMeterUsage(ctx context.Context, records []*MeterUsage) error

	// IsDuplicate checks if a meter usage record with the given unique_hash already exists for the meter
	IsDuplicate(ctx context.Context, meterID, uniqueHash string) (bool, error)

	// GetUsage queries aggregated usage for a single meter
	GetUsage(ctx context.Context, params *MeterUsageQueryParams) (*MeterUsageAggregationResult, error)

	// GetUsageMultiMeter queries aggregated usage for multiple meters, returning one result per meter
	GetUsageMultiMeter(ctx context.Context, params *MeterUsageQueryParams) ([]*MeterUsageAggregationResult, error)

	// GetUsageForBucketedMeters returns windowed aggregation results for bucketed meters (MAX/SUM with bucket_size).
	// Returns *AggregationResult (shared type with feature_usage) for compatibility with calculateBucketedMeterCost.
	GetUsageForBucketedMeters(ctx context.Context, params *MeterUsageQueryParams) (*AggregationResult, error)

	// GetDistinctMeterIDs returns the set of meter_ids that have data in the meter_usage table
	// for the given customer(s) and time range. Used to skip meters with zero usage.
	GetDistinctMeterIDs(ctx context.Context, params *MeterUsageQueryParams) ([]string, error)

	// GetDetailedAnalytics provides comprehensive analytics with filtering, grouping, and time-series data
	GetDetailedAnalytics(ctx context.Context, params *MeterUsageDetailedAnalyticsParams) ([]*MeterUsageDetailedResult, error)
}
