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
	GroupBy            []string // Allowed values: "source", "feature_id"
	WindowSize         types.WindowSize
	PropertyFilters    map[string][]string
}

// DetailedUsageAnalytic represents detailed usage and cost data for analytics
type DetailedUsageAnalytic struct {
	FeatureID       string
	FeatureName     string
	EventName       string
	Source          string
	MeterID         string
	AggregationType types.AggregationType
	Unit            string
	UnitPlural      string
	TotalUsage      decimal.Decimal
	TotalCost       decimal.Decimal
	Currency        string
	EventCount      uint64 // Number of events that contributed to this aggregation
	Points          []UsageAnalyticPoint
}

// UsageAnalyticPoint represents a data point in a time series
type UsageAnalyticPoint struct {
	Timestamp  time.Time
	Usage      decimal.Decimal
	Cost       decimal.Decimal
	EventCount uint64 // Number of events in this time window
}
