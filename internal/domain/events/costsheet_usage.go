package events

import (
	"context"
	"time"
)

// FeatureUsageRepository defines operations for feature usage tracking
type CostSheetUsageRepository interface {
	// Bulk insert events into events_processed table
	BulkInsertProcessedEvents(ctx context.Context, events []*CostUsage) error

	// Get processed events with filtering options
	GetProcessedEvents(ctx context.Context, params *GetCostUsageEventsParams) ([]*CostUsage, uint64, error)

	// Get usage by cost sheet ID
	GetUsageByCostSheetID(ctx context.Context, costSheetID, externalCustomerID string, startTime, endTime time.Time) (map[string]*UsageByCostSheetResult, error)

	// GetDetailedUsageAnalytics provides comprehensive usage analytics with filtering, grouping, and time-series data
	GetDetailedUsageAnalytics(ctx context.Context, costSheetID, externalCustomerID string, params *UsageAnalyticsParams, maxBucketFeatures map[string]*MaxBucketFeatureInfo) ([]*DetailedUsageAnalytic, error)
}
