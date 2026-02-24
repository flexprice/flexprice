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

const RawEventsActivityPrefix = "RawEventsActivities"

// ReprocessRawEventsActivities contains all raw event reprocessing activities
type ReprocessRawEventsActivities struct {
	rawEventsReprocessingService service.RawEventsReprocessingService
}

// NewReprocessRawEventsActivities creates a new ReprocessRawEventsActivities instance
func NewReprocessRawEventsActivities(rawEventsReprocessingService service.RawEventsReprocessingService) *ReprocessRawEventsActivities {
	return &ReprocessRawEventsActivities{
		rawEventsReprocessingService: rawEventsReprocessingService,
	}
}

// ReprocessRawEvents reprocesses raw events
// This method will be registered as "ReprocessRawEvents" in Temporal
func (a *ReprocessRawEventsActivities) ReprocessRawEvents(ctx context.Context, input models.ReprocessRawEventsWorkflowInput) (*models.ReprocessRawEventsWorkflowResult, error) {
	logger := activity.GetLogger(ctx)
	response := &models.ReprocessRawEventsWorkflowResult{
		TotalEventsFound:          0,
		TotalEventsPublished:      0,
		TotalEventsFailed:         0,
		TotalEventsDropped:        0,
		TotalTransformationErrors: 0,
		ProcessedBatches:          0,
	}

	// Validate input
	if err := input.Validate(); err != nil {
		return response, err
	}

	// Set context values using centralized utilities
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)
	ctx = types.SetUserID(ctx, input.UserID)

	logger.Info("Starting reprocess raw events activity",
		"external_customer_id", input.ExternalCustomerID,
		"event_name", input.EventName,
		"start_date", input.StartDate,
		"end_date", input.EndDate,
		"batch_size", input.BatchSize)

	// Convert workflow input to service params
	reprocessParams := &events.ReprocessRawEventsParams{
		ExternalCustomerID:  input.ExternalCustomerID,
		ExternalCustomerIDs: input.ExternalCustomerIDs,
		EventName:           input.EventName,
		StartTime:           input.StartDate,
		EndTime:             input.EndDate,
		BatchSize:           input.BatchSize,
		EventIDs:            input.EventIDs,
		UseUnprocessed:      input.UseUnprocessed,
	}

	// Call the service method to reprocess raw events
	result, err := a.rawEventsReprocessingService.ReprocessRawEvents(ctx, reprocessParams)
	if err != nil {
		logger.Error("Failed to reprocess raw events",
			"external_customer_id", input.ExternalCustomerID,
			"event_name", input.EventName,
			"error", err)
		return response, ierr.WithError(err).
			WithHint("Failed to reprocess raw events").
			WithReportableDetails(map[string]interface{}{
				"external_customer_id": input.ExternalCustomerID,
				"event_name":           input.EventName,
			}).
			Mark(ierr.ErrInternal)
	}

	logger.Info("Completed reprocess raw events activity",
		"external_customer_id", input.ExternalCustomerID,
		"event_name", input.EventName,
		"total_events_found", result.TotalEventsFound,
		"total_events_published", result.TotalEventsPublished,
		"total_events_dropped", result.TotalEventsDropped,
		"total_transformation_errors", result.TotalTransformationErrors,
		"total_events_failed", result.TotalEventsFailed,
		"processed_batches", result.ProcessedBatches)

	// Map service result to workflow result
	response.TotalEventsFound = result.TotalEventsFound
	response.TotalEventsPublished = result.TotalEventsPublished
	response.TotalEventsFailed = result.TotalEventsFailed
	response.TotalEventsDropped = result.TotalEventsDropped
	response.TotalTransformationErrors = result.TotalTransformationErrors
	response.ProcessedBatches = result.ProcessedBatches

	return response, nil
}
