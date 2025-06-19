package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalsdk "go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// StripeEventSyncWorkflow orchestrates the hourly synchronization of events to Stripe
func StripeEventSyncWorkflow(ctx workflow.Context, input models.StripeEventSyncWorkflowInput) (*models.StripeEventSyncWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting Stripe event sync workflow",
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID,
		"window_start", input.WindowStart,
		"window_end", input.WindowEnd,
		"batch_size_limit", input.BatchSizeLimit,
		"grace_period", input.GracePeriod)

	// Validate input
	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return nil, err
	}

	// Set up activity options with configurable timeouts and retry policies
	// Use configurable values or sensible defaults
	maxRetries := input.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3 // Default from StripeConfig
	}

	apiTimeout := input.APITimeout
	if apiTimeout == 0 {
		apiTimeout = 30 * time.Second // Default from StripeConfig
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout:    apiTimeout * 2, // Give activities 2x API timeout
		ScheduleToCloseTimeout: apiTimeout * 3, // Total timeout including retries
		RetryPolicy: &temporalsdk.RetryPolicy{
			InitialInterval:    time.Second * 5,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 2,
			MaximumAttempts:    int32(maxRetries),
			NonRetryableErrorTypes: []string{
				"ValidationError", // Don't retry validation errors
			},
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	result := &models.StripeEventSyncWorkflowResult{
		TotalBatches:      0,
		SuccessfulBatches: 0,
		FailedBatches:     0,
		TotalEvents:       0,
		TotalQuantity:     0,
		ProcessedAt:       workflow.Now(ctx),
		Errors:            []string{},
	}

	// Step 1: Aggregate events from ClickHouse
	aggregateInput := models.AggregateEventsActivityInput{
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		WindowStart:   input.WindowStart,
		WindowEnd:     input.WindowEnd,
		BatchSize:     input.BatchSizeLimit,
	}

	var aggregateResult models.AggregateEventsActivityResult
	err := workflow.ExecuteActivity(ctx, "AggregateEventsActivity", aggregateInput).Get(ctx, &aggregateResult)
	if err != nil {
		logger.Error("Failed to aggregate events", "error", err)
		result.Errors = append(result.Errors, "Event aggregation failed: "+err.Error())
		return result, err
	}

	result.TotalEvents = aggregateResult.TotalEvents
	result.TotalQuantity = aggregateResult.TotalQuantity

	logger.Info("Event aggregation completed",
		"aggregations_count", len(aggregateResult.Aggregations),
		"total_events", aggregateResult.TotalEvents,
		"total_quantity", aggregateResult.TotalQuantity)

	// If no events to sync, return early
	if len(aggregateResult.Aggregations) == 0 {
		logger.Info("No events found for synchronization")
		return result, nil
	}

	// Step 2: Sync aggregated events to Stripe
	syncInput := models.SyncToStripeActivityInput{
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		Aggregations:  aggregateResult.Aggregations,
		WindowStart:   input.WindowStart,
		WindowEnd:     input.WindowEnd,
	}

	var syncResult models.SyncToStripeActivityResult
	err = workflow.ExecuteActivity(ctx, "SyncToStripeActivity", syncInput).Get(ctx, &syncResult)
	if err != nil {
		logger.Error("Failed to sync events to Stripe", "error", err)
		result.Errors = append(result.Errors, "Stripe sync failed: "+err.Error())
		// Continue to tracking even if sync failed to record the attempt
	} else {
		result.SuccessfulBatches = syncResult.SuccessfulSyncs
		result.FailedBatches = syncResult.FailedSyncs
		result.TotalBatches = len(syncResult.SyncedBatches)

		logger.Info("Stripe sync completed",
			"successful_syncs", syncResult.SuccessfulSyncs,
			"failed_syncs", syncResult.FailedSyncs)
	}

	// Step 3: Track sync batch results in database
	trackInput := models.TrackSyncBatchActivityInput{
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		SyncResults:   syncResult.SyncedBatches,
		WindowStart:   input.WindowStart,
		WindowEnd:     input.WindowEnd,
	}

	var trackResult models.TrackSyncBatchActivityResult
	err = workflow.ExecuteActivity(ctx, "TrackSyncBatchActivity", trackInput).Get(ctx, &trackResult)
	if err != nil {
		logger.Error("Failed to track sync batches", "error", err)
		result.Errors = append(result.Errors, "Batch tracking failed: "+err.Error())
		// Don't fail the workflow if tracking fails
	} else {
		logger.Info("Batch tracking completed",
			"tracked_batches", trackResult.TrackedBatches,
			"successful_tracks", trackResult.SuccessfulTracks)
	}

	// Log final results
	logger.Info("Stripe event sync workflow completed",
		"tenant_id", input.TenantID,
		"total_batches", result.TotalBatches,
		"successful_batches", result.SuccessfulBatches,
		"failed_batches", result.FailedBatches,
		"total_events", result.TotalEvents,
		"errors_count", len(result.Errors))

	return result, nil
}

// CronStripeEventSyncWorkflow is a cron-based workflow that triggers hourly Stripe synchronization
func CronStripeEventSyncWorkflow(ctx workflow.Context, input models.StripeEventSyncWorkflowInput) (*models.StripeEventSyncWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting cron Stripe event sync workflow", "tenant_id", input.TenantID)

	// Calculate the time window for the previous hour
	now := workflow.Now(ctx)

	// Apply grace period - sync events from (current_hour - 1 - grace_period) to (current_hour - 1)
	endTime := now.Add(-time.Hour).Truncate(time.Hour)
	startTime := endTime.Add(-time.Hour)

	// Apply grace period to start time to ensure we don't miss late-arriving events
	if input.GracePeriod > 0 {
		startTime = startTime.Add(-input.GracePeriod)
	}

	// Update input with calculated time window
	syncInput := input
	syncInput.WindowStart = startTime
	syncInput.WindowEnd = endTime

	logger.Info("Calculated sync time window",
		"window_start", startTime,
		"window_end", endTime,
		"grace_period", input.GracePeriod)

	// Execute the main sync workflow
	return StripeEventSyncWorkflow(ctx, syncInput)
}

// ManualStripeEventSyncWorkflow allows manual triggering of Stripe sync for specific time ranges
func ManualStripeEventSyncWorkflow(ctx workflow.Context, input models.StripeEventSyncWorkflowInput) (*models.StripeEventSyncWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting manual Stripe event sync workflow",
		"tenant_id", input.TenantID,
		"window_start", input.WindowStart,
		"window_end", input.WindowEnd)

	// For manual sync, use the provided time window as-is
	return StripeEventSyncWorkflow(ctx, input)
}
