package events

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type Repository interface {
	InsertEvent(ctx context.Context, event *Event) error
	BulkInsertEvents(ctx context.Context, events []*Event) error
	GetUsage(ctx context.Context, params *UsageParams) (*AggregationResult, error)
	GetUsageWithFilters(ctx context.Context, params *UsageWithFiltersParams) ([]*AggregationResult, error)
	GetEvents(ctx context.Context, params *GetEventsParams) ([]*Event, uint64, error)
	GetEventByID(ctx context.Context, eventID string) (*Event, error)
	FindUnprocessedEvents(ctx context.Context, params *FindUnprocessedEventsParams) ([]*Event, error)
	FindUnprocessedEventsFromFeatureUsage(ctx context.Context, params *FindUnprocessedEventsParams) ([]*Event, error)
	GetDistinctEventNames(ctx context.Context, externalCustomerIDs []string, startTime, endTime time.Time) ([]string, error)
	GetDistinctExternalCustomerIDs(ctx context.Context, startTime, endTime time.Time) ([]string, error)

	// Monitoring methods
	GetTotalEventCount(ctx context.Context, startTime, endTime time.Time, windowSize types.WindowSize) (*EventCountResult, error)
}

// ProcessedEventRepository defines operations for processed events
type ProcessedEventRepository interface {
	// Inserts a single processed event into events_processed table
	InsertProcessedEvent(ctx context.Context, event *ProcessedEvent) error

	// Bulk insert events into events_processed table
	BulkInsertProcessedEvents(ctx context.Context, events []*ProcessedEvent) error

	// Get processed events with filtering options
	GetProcessedEvents(ctx context.Context, params *GetProcessedEventsParams) ([]*ProcessedEvent, uint64, error)

	// Check for duplicate event using unique_hash
	IsDuplicate(ctx context.Context, subscriptionID, meterID string, periodID uint64, uniqueHash string) (bool, error)

	// Get free and billable quantity to date for a subscription line item
	GetLineItemUsage(ctx context.Context, subLineItemID string, periodID uint64) (qty decimal.Decimal, freeUnits decimal.Decimal, err error)

	// Get usage cost to date for a customer's subscription in a billing period
	GetPeriodCost(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) (decimal.Decimal, error)

	// Get usage totals per feature for invoicing
	GetPeriodFeatureTotals(ctx context.Context, tenantID, environmentID, customerID, subscriptionID string, periodID uint64) ([]*PeriodFeatureTotal, error)

	// Get usage analytics for recent events
	GetUsageAnalytics(ctx context.Context, tenantID, environmentID, customerID string, lookbackHours int) ([]*UsageAnalytic, error)

	// GetDetailedUsageAnalytics provides comprehensive usage analytics with filtering, grouping, and time-series data
	GetDetailedUsageAnalytics(ctx context.Context, params *UsageAnalyticsParams) ([]*DetailedUsageAnalytic, error)
}

// RawEventRepository defines operations for raw events
type RawEventRepository interface {
	// FindRawEvents finds raw events with filtering and keyset pagination
	FindRawEvents(ctx context.Context, params *FindRawEventsParams) ([]*RawEvent, error)

	// FindUnprocessedRawEvents finds raw events that haven't been processed yet
	// Uses ANTI JOIN with feature_usage table to exclude already processed events
	FindUnprocessedRawEvents(ctx context.Context, params *FindRawEventsParams) ([]*RawEvent, *KeysetCursor, error)
}

// Additional types needed for the new methods

// PeriodFeatureTotal represents aggregated usage for a feature in a period
type PeriodFeatureTotal struct {
	FeatureID string          `json:"feature_id"`
	Quantity  decimal.Decimal `json:"quantity" swaggertype:"string"`
	FreeUnits decimal.Decimal `json:"free_units" swaggertype:"string"`
	Cost      decimal.Decimal `json:"cost" swaggertype:"string"`
}

// UsageAnalytic represents usage analytics data grouped by source and feature
type UsageAnalytic struct {
	Source    string          `json:"source"`
	FeatureID string          `json:"feature_id"`
	Cost      decimal.Decimal `json:"cost" swaggertype:"string"`
	Usage     decimal.Decimal `json:"usage" swaggertype:"string"`
}

type UsageParams struct {
	ExternalCustomerID  string                `json:"external_customer_id"`
	ExternalCustomerIDs []string              `json:"-" form:"-"`
	CustomerID          string                `json:"customer_id"`
	EventName           string                `json:"event_name" validate:"required"`
	PropertyName        string                `json:"property_name" validate:"required"`
	AggregationType     types.AggregationType `json:"aggregation_type" validate:"required"`
	WindowSize          types.WindowSize      `json:"window_size"`
	BucketSize          types.WindowSize      `json:"bucket_size,omitempty"` // For windowed MAX aggregation
	StartTime           time.Time             `json:"start_time" validate:"required"`
	EndTime             time.Time             `json:"end_time" validate:"required"`
	Filters             map[string][]string   `json:"filters"`
	Multiplier          *decimal.Decimal      `json:"multiplier,omitempty" validate:"omitempty,gt=0"`
	// BillingAnchor enables custom monthly billing periods for usage aggregation.
	//
	// Behavior by WindowSize:
	// - MONTH + BillingAnchor: Custom monthly periods (e.g., 5th to 5th of each month)
	// - MONTH + nil: Standard calendar months (1st to 1st of each month)
	// - DAY/HOUR/WEEK/etc: BillingAnchor is ignored, uses standard windows
	//
	// Precision: Day-level granularity is used for simplicity and predictability (time is ignored)
	// Example: BillingAnchor = 2024-03-05 (any time on March 5th)
	//   - Creates monthly buckets starting on the 5th of each month
	//
	// Use cases:
	// - Subscription billing periods that don't align with calendar months
	// - Custom business cycles (fiscal months, quarterly periods)
	// - Multi-tenant billing with different anchor dates per customer
	BillingAnchor *time.Time `json:"billing_anchor,omitempty"`
	// GroupBy lists the analytics group_by dimensions.
	//   - "source"        — group by event source
	//   - "properties.X"  — group by JSON property X
	// Naming matches UsageAnalyticsParams.GroupBy + MeterUsageQueryParams.GroupBy.
	// Currently only supported for MAX aggregation with bucket_size; the events
	// repo's per-row SQL consumes the first dim for legacy single-property
	// grouping (multi-dim support is on the bucketed-meter path).
	GroupBy []string `json:"group_by,omitempty"`
}

// UsageSummaryParams defines parameters for querying pre-computed usage
type UsageSummaryParams struct {
	StartTime      time.Time `json:"start_time" validate:"required"`
	EndTime        time.Time `json:"end_time" validate:"required"`
	CustomerID     string    `json:"customer_id"`
	SubscriptionID string    `json:"subscription_id"`
	MeterID        string    `json:"meter_id"`
	PriceID        string    `json:"price_id"`
	FeatureID      string    `json:"feature_id"`
}

// GetProcessedEventsParams defines parameters for querying processed events
type GetProcessedEventsParams struct {
	StartTime      time.Time `json:"start_time" validate:"required"`
	EndTime        time.Time `json:"end_time" validate:"required"`
	CustomerID     string    `json:"customer_id"`
	SubscriptionID string    `json:"subscription_id"`
	MeterID        string    `json:"meter_id"`
	FeatureID      string    `json:"feature_id"`
	PriceID        string    `json:"price_id"`
	Offset         int       `json:"offset"`
	Limit          int       `json:"limit"`
	CountTotal     bool      `json:"count_total"`
}

type GetEventsParams struct {
	ExternalCustomerID string              `json:"external_customer_id"`
	EventName          string              `json:"event_name" validate:"required"`
	EventID            string              `json:"event_id"`
	StartTime          time.Time           `json:"start_time" validate:"required"`
	EndTime            time.Time           `json:"end_time" validate:"required"`
	IterFirst          *EventIterator      `json:"iter_first"`
	IterLast           *EventIterator      `json:"iter_last"`
	PageSize           int                 `json:"page_size"`
	PropertyFilters    map[string][]string `json:"property_filters,omitempty"`
	Offset             int                 `json:"offset"`
	Source             string              `json:"source"`
	Sort               *string             `json:"sort"`
	Order              *string             `json:"order"`
	CountTotal         bool                `json:"count_total"`
}

type UsageResult struct {
	WindowSize time.Time       `json:"window_size"`
	Value      decimal.Decimal `json:"value"`
	EventCount uint64          `json:"event_count"`         // Distinct event count in this window
	GroupKey   string          `json:"group_key,omitempty"` // group identifier when group_by is used (e.g., KRN value)
}

// FirstGroupByProperty extracts the bare property name from the FIRST entry of
// a GroupBy list when that entry is a "properties.X" form. Returns "" when the
// list is empty or the first dim isn't a "properties.X" form.
//
// Positional semantics matter here: legacy SQL paths only support a single
// dimension, and the primary dim is whatever the user listed first. Treating
// later entries as candidates would silently demote the user's primary dim
// (e.g., ["source", "properties.org_id"] would otherwise group by org_id and
// drop source, which is the opposite of what the user asked).
func FirstGroupByProperty(groupBy []string) string {
	if len(groupBy) == 0 {
		return ""
	}
	const prefix = "properties."
	first := groupBy[0]
	if len(first) > len(prefix) && first[:len(prefix)] == prefix {
		return first[len(prefix):]
	}
	return ""
}

type AggregationResult struct {
	Results    []UsageResult         `json:"results,omitempty"`
	Value      decimal.Decimal       `json:"value,omitempty"`
	EventCount uint64                `json:"event_count"` // Total distinct event count across all results
	EventName  string                `json:"event_name"`
	Type       types.AggregationType `json:"type"`
	Metadata   map[string]string     `json:"metadata,omitempty"`
	MeterID    string                `json:"meter_id"`
	PriceID    string                `json:"price_id"`
}

type EventIterator struct {
	Timestamp time.Time
	ID        string
}

// EventCountPoint represents a single time-series data point for event counts
type EventCountPoint struct {
	Timestamp  time.Time `json:"timestamp"`
	EventCount uint64    `json:"event_count"`
}

// EventCountResult represents the result of a windowed event count query
type EventCountResult struct {
	TotalCount uint64            `json:"total_count"`
	Points     []EventCountPoint `json:"points,omitempty"`
}

// FilterGroup represents a group of filters with priority
type FilterGroup struct {
	// ID is the identifier for the filter group. We are using the price ID
	// as the unique identifier for the filter group as of now
	ID string `json:"id"`

	// Priority is the priority of the filter group for deduping events matching multiple filter groups
	Priority int `json:"priority"`

	// Filters are the actual filters where the key is the $properties.key
	// and the values are all the predefined filter values
	Filters map[string][]string `json:"filters"`
}

type UsageWithFiltersParams struct {
	*UsageParams
	FilterGroups []FilterGroup // Ordered list of filter groups, from most specific to least specific
}

type FeatureUsageParams struct {
	*UsageParams
	FeatureID     string `json:"feature_id"`
	PriceID       string `json:"price_id"`
	MeterID       string `json:"meter_id"`
	SubLineItemID string `json:"sub_line_item_id"`
	// QuerySource: when InvoiceCreation, ClickHouse uses FINAL for ReplacingMergeTree deduplication; other sources do not.
	Source types.UsageSource `json:"query_source,omitempty"`
}

// Cost Usage Params

type GetCostUsageEventsParams struct {
	StartTime   time.Time `json:"start_time" validate:"required"`
	EndTime     time.Time `json:"end_time" validate:"required"`
	CustomerID  string    `json:"customer_id"`
	CostSheetID string    `json:"costsheet_id"`
	MeterID     string    `json:"meter_id"`
	FeatureID   string    `json:"feature_id"`
	PriceID     string    `json:"price_id"`
	Offset      int       `json:"offset"`
	Limit       int       `json:"limit"`
	CountTotal  bool      `json:"count_total"`
}
