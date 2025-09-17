package events

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// FeatureUsageTrackingRepository defines operations for feature usage tracking
type FeatureUsageRepository interface {
	// Inserts a single processed event into events_processed table
	InsertProcessedEvent(ctx context.Context, event *FeatureUsage) error

	// Bulk insert events into events_processed table
	BulkInsertProcessedEvents(ctx context.Context, events []*FeatureUsage) error

	// Get processed events with filtering options
	GetProcessedEvents(ctx context.Context, params *GetProcessedEventsParams) ([]*FeatureUsage, uint64, error)

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

	// Get feature usage by subscription
	GetFeatureUsageBySubscription(ctx context.Context, subscriptionID, externalCustomerID, environmentID, tenantID string, startTime, endTime time.Time) (map[string]*UsageByFeatureResult, error)
}
