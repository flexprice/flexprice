package events

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/service"
	models "github.com/flexprice/flexprice/internal/temporal/models/events"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/activity"
)

const EventsActivityPrefix = "EventsActivities"

// ReprocessEventsActivities contains all event reprocessing activities
type ReprocessEventsActivities struct {
	featureUsageTrackingService service.FeatureUsageTrackingService
}

// NewReprocessEventsActivities creates a new ReprocessEventsActivities instance
func NewReprocessEventsActivities(featureUsageTrackingService service.FeatureUsageTrackingService) *ReprocessEventsActivities {
	return &ReprocessEventsActivities{
		featureUsageTrackingService: featureUsageTrackingService,
	}
}

// ReprocessEvents reprocesses events for feature usage tracking
// This method will be registered as "ReprocessEvents" in Temporal
func (a *ReprocessEventsActivities) ReprocessEvents(ctx context.Context, input models.ReprocessEventsWorkflowInput) (*models.ReprocessEventsWorkflowResult, error) {
	logger := activity.GetLogger(ctx)
	response := &models.ReprocessEventsWorkflowResult{
		TotalEventsFound:     0,
		TotalEventsPublished: 0,
		ProcessedBatches:     0,
	}

	// Validate input
	if err := input.Validate(); err != nil {
		return response, err
	}

	// Set context values using centralized utilities
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	logger.Info("Starting reprocess events activity",
		"external_customer_id", input.ExternalCustomerID,
		"event_name", input.EventName,
		"start_date", input.StartDate,
		"end_date", input.EndDate,
		"batch_size", input.BatchSize)

	// Convert workflow input to service params
	reprocessParams := &events.ReprocessEventsParams{
		ExternalCustomerID: input.ExternalCustomerID,
		EventName:          input.EventName,
		StartTime:          input.StartDate,
		EndTime:            input.EndDate,
		BatchSize:          input.BatchSize,
	}

	// Call the service method to reprocess events
	result, err := a.featureUsageTrackingService.ReprocessEvents(ctx, reprocessParams)
	if err != nil {
		logger.Error("Failed to reprocess events",
			"external_customer_id", input.ExternalCustomerID,
			"event_name", input.EventName,
			"error", err)
		return response, ierr.WithError(err).
			WithHint("Failed to reprocess events for feature usage tracking").
			WithReportableDetails(map[string]interface{}{
				"external_customer_id": input.ExternalCustomerID,
				"event_name":           input.EventName,
			}).
			Mark(ierr.ErrInternal)
	}

	logger.Info("Completed reprocess events activity",
		"external_customer_id", input.ExternalCustomerID,
		"event_name", input.EventName,
		"total_events_found", result.TotalEventsFound,
		"total_events_published", result.TotalEventsPublished,
		"processed_batches", result.ProcessedBatches)

	// Populate response from service result
	response.TotalEventsFound = result.TotalEventsFound
	response.TotalEventsPublished = result.TotalEventsPublished
	response.ProcessedBatches = result.ProcessedBatches

	return response, nil
}
