package events

import (
	"context"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
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

	// Get feature usage by subscription. opts.Source controls whether ClickHouse uses FINAL (e.g. invoice_creation).
	GetFeatureUsageBySubscription(ctx context.Context, subscriptionID, customerID string, startTime, endTime time.Time, aggTypes []types.AggregationType, opts *GetFeatureUsageBySubscriptionOpts) (map[string]*UsageByFeatureResult, error)

	// GetFeatureUsageForExport gets feature usage data for export in batches
	GetFeatureUsageForExport(ctx context.Context, startTime, endTime time.Time, batchSize int, offset int) ([]*FeatureUsage, error)

	GetUsageForMaxMetersWithBuckets(ctx context.Context, params *FeatureUsageParams) (*AggregationResult, error)

	// GetFeatureUsageByEventIDs gets feature usage records by event IDs
	GetFeatureUsageByEventIDs(ctx context.Context, eventIDs []string) ([]*FeatureUsage, error)

	// DeleteByReprocessScopeBeforeCheckpoint cleans old rows for a scope using processed_at checkpoint fence.
	DeleteByReprocessScopeBeforeCheckpoint(ctx context.Context, params *DeleteFeatureUsageScopeParams) error
}

// DeleteFeatureUsageScopeParams defines cleanup scope for reprocessing.
type DeleteFeatureUsageScopeParams struct {
	GetEventsParams *GetEventsParams
	RunStartTime    time.Time
}

func (p *DeleteFeatureUsageScopeParams) Validate() error {
	if p.GetEventsParams == nil {
		return ierr.NewError("get events params is required").
			WithHint("Get events params is required").
			Mark(ierr.ErrValidation)
	}

	if p.RunStartTime.IsZero() {
		return ierr.NewError("run start time is required").
			WithHint("Run start time is required").
			Mark(ierr.ErrValidation)
	}

	if p.GetEventsParams.StartTime.IsZero() {
		return ierr.NewError("start time is required").
			WithHint("Start time is required").
			Mark(ierr.ErrValidation)
	}

	if p.GetEventsParams.EndTime.IsZero() {
		return ierr.NewError("end time is required").
			WithHint("End time is required").
			Mark(ierr.ErrValidation)
	}

	if p.GetEventsParams.StartTime.After(p.GetEventsParams.EndTime) {
		return ierr.NewError("start time must be before end time").
			WithHint("Start time must be before end time").
			Mark(ierr.ErrValidation)
	}
	if p.GetEventsParams.ExternalCustomerID == "" {
		return ierr.NewError("external customer id is required").
			WithHint("External customer id is required").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// GetFeatureUsageBySubscriptionOpts holds options for GetFeatureUsageBySubscription.
// When Source is InvoiceCreation, the ClickHouse query uses FINAL for correct ReplacingMergeTree deduplication.
type GetFeatureUsageBySubscriptionOpts struct {
	Source types.UsageSource
}

// MaxBucketFeatureInfo contains information about a feature that uses MAX with bucket aggregation
type MaxBucketFeatureInfo struct {
	FeatureID       string
	MeterID         string
	BucketSize      types.WindowSize
	EventName       string
	PropertyName    string
	GroupByProperty string // Property name in event.properties to group by before aggregating
}

// SumBucketFeatureInfo contains information about a feature that uses SUM with bucket aggregation
type SumBucketFeatureInfo struct {
	FeatureID    string
	MeterID      string
	BucketSize   types.WindowSize
	EventName    string
	PropertyName string
}
