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

// MeterUsageQueryResult holds the combined results of a QueryUsageByMeters call.
// Regular contains scalar results for non-bucketed meters, keyed by meter_id.
// Bucketed contains windowed results for bucketed meters (MAX/SUM with BucketSize), keyed by meter_id.
// Only meters with actual data in the period are present — meters with zero usage are omitted.
type MeterUsageQueryResult struct {
	Regular  map[string]*MeterUsageAggregationResult
	Bucketed map[string]*AggregationResult
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
}
