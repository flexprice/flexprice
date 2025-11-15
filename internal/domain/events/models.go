package events

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// UsageAnalyticsParams defines parameters for detailed usage analytics queries
type UsageAnalyticsParams struct {
	TenantID           string
	EnvironmentID      string
	CustomerID         string
	ExternalCustomerID string
	FeatureIDs         []string
	Sources            []string
	StartTime          time.Time
	EndTime            time.Time
	GroupBy            []string // Allowed values: "source", "feature_id", "properties.<field_name>"
	WindowSize         types.WindowSize
	PropertyFilters    map[string][]string
	// BillingAnchor defines the reference point for custom billing periods.
	// Only affects MONTH window size - all other window sizes ignore this field.
	//
	// When WindowSize = MONTH and BillingAnchor is provided:
	// - Events are grouped by custom monthly periods (e.g., 5th to 5th of each month)
	// - Only the day component is used for simplicity and predictability (time is ignored)
	// - Example: BillingAnchor = 2024-03-05 (any time on March 5th)
	//   - Events from March 5 to April 5 → March billing period
	//   - Events from April 5 to May 5 → April billing period
	//
	// When WindowSize != MONTH or BillingAnchor is nil:
	// - Falls back to standard calendar-based windows (1st to 1st for months)
	// - DAY: 00:00:00 to 23:59:59, HOUR: 00:00 to 59:59, etc.
	//
	// Use cases:
	// - Monthly billing that doesn't align with calendar months
	// - Subscription billing periods (e.g., customer signed up on 15th)
	// - Custom business cycles (e.g., fiscal months starting on 5th)
	BillingAnchor *time.Time
}

// DetailedUsageAnalytic represents detailed usage and cost data for analytics
type DetailedUsageAnalytic struct {
	FeatureID       string
	FeatureName     string
	EventName       string
	Source          string
	MeterID         string
	PriceID         string // Price ID used for this usage - allows tracking different prices per subscription
	SubLineItemID   string // Subscription line item ID
	SubscriptionID  string // Subscription ID
	AggregationType types.AggregationType
	Unit            string
	UnitPlural      string
	TotalUsage      decimal.Decimal
	TotalCost       decimal.Decimal
	Currency        string
	EventCount      uint64            // Number of events that contributed to this aggregation
	Properties      map[string]string // Stores property values for flexible grouping (e.g., org_id -> "org123")
	Points          []UsageAnalyticPoint

	// All aggregation values - we fetch all and use the appropriate one based on meter type
	MaxUsage         decimal.Decimal // MAX(qty_total * sign)
	LatestUsage      decimal.Decimal // argMax(qty_total, timestamp)
	CountUniqueUsage uint64          // COUNT(DISTINCT unique_hash)
}

// UsageAnalyticPoint represents a data point in a time series
type UsageAnalyticPoint struct {
	Timestamp  time.Time
	Usage      decimal.Decimal
	Cost       decimal.Decimal
	EventCount uint64 // Number of events in this time window

	// All aggregation values for this time point
	MaxUsage         decimal.Decimal // MAX(qty_total * sign)
	LatestUsage      decimal.Decimal // argMax(qty_total, timestamp)
	CountUniqueUsage uint64          // COUNT(DISTINCT unique_hash)
}

// UsageByFeatureResult represents aggregated usage data for a feature
type UsageByFeatureResult struct {
	SubLineItemID    string
	FeatureID        string
	MeterID          string
	PriceID          string
	SumTotal         decimal.Decimal
	MaxTotal         decimal.Decimal
	CountDistinctIDs uint64
	CountUniqueQty   uint64
	LatestQty        decimal.Decimal
}
