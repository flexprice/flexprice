package events

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// FeatureUsageRepository defines operations for feature usage tracking
type FeatureUsageRepository interface {
	// Inserts a single processed event into events_processed table
	InsertProcessedEvent(ctx context.Context, event *FeatureUsage) error

	// Bulk insert events into events_processed table
	BulkInsertProcessedEvents(ctx context.Context, events []*FeatureUsage) error

	// Get processed events with filtering options
	GetProcessedEvents(ctx context.Context, params *GetProcessedEventsParams) ([]*FeatureUsage, uint64, error)

	// Check for duplicate event using unique_hash
	IsDuplicate(ctx context.Context, subscriptionID, meterID string, periodID uint64, uniqueHash string) (bool, error)

	// GetDetailedUsageAnalytics provides comprehensive usage analytics with filtering, grouping, and time-series data
	GetDetailedUsageAnalytics(ctx context.Context, params *UsageAnalyticsParams, maxBucketFeatures map[string]*MaxBucketFeatureInfo, sumBucketFeatures map[string]*SumBucketFeatureInfo) ([]*DetailedUsageAnalytic, error)

	// Get feature usage by subscription
	GetFeatureUsageBySubscription(ctx context.Context, subscriptionID, externalCustomerID string, startTime, endTime time.Time) (map[string]*UsageByFeatureResult, error)

	// GetFeatureUsageForExport gets feature usage data for export in batches
	GetFeatureUsageForExport(ctx context.Context, startTime, endTime time.Time, batchSize int, offset int) ([]*FeatureUsage, error)

	GetUsageForMaxMetersWithBuckets(ctx context.Context, params *FeatureUsageParams) (*AggregationResult, error)

	// GetFeatureUsageByEventIDs gets feature usage records by event IDs
	GetFeatureUsageByEventIDs(ctx context.Context, eventIDs []string, externalCustomerID string) ([]*FeatureUsage, error)
}

// MaxBucketFeatureInfo contains information about a feature that uses MAX with bucket aggregation
type MaxBucketFeatureInfo struct {
	FeatureID    string
	MeterID      string
	BucketSize   types.WindowSize
	EventName    string
	PropertyName string
}

// SumBucketFeatureInfo contains information about a feature that uses SUM with bucket aggregation
type SumBucketFeatureInfo struct {
	FeatureID    string
	MeterID      string
	BucketSize   types.WindowSize
	EventName    string
	PropertyName string
}
