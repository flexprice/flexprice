package dlq

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	dlqengine "github.com/flexprice/flexprice/internal/kafka/dlq"
	models "github.com/flexprice/flexprice/internal/temporal/models/dlq"
	"go.temporal.io/sdk/activity"
)

// ReplayDLQActivities contains the DLQ replay activity.
type ReplayDLQActivities struct {
	dlqReplayService service.DLQReplayService
}

// NewReplayDLQActivities creates a new ReplayDLQActivities instance.
func NewReplayDLQActivities(dlqReplayService service.DLQReplayService) *ReplayDLQActivities {
	return &ReplayDLQActivities{dlqReplayService: dlqReplayService}
}

// ReplayDLQ drains one DLQ topic back to origin.
// This method will be registered as "ReplayDLQ" in Temporal.
func (a *ReplayDLQActivities) ReplayDLQ(ctx context.Context, input models.ReplayDLQWorkflowInput) (*models.ReplayDLQWorkflowResult, error) {
	logger := activity.GetLogger(ctx)

	if err := input.Validate(); err != nil {
		return nil, err
	}

	logger.Info("Starting DLQ replay activity",
		"source_topic", input.SourceTopic,
		"dry_run", input.DryRun,
		"since_ms", input.SinceMs,
		"from_start", input.FromStart)

	summary, err := a.dlqReplayService.ReplayDLQ(ctx, dlqengine.Options{
		SourceTopic:    input.SourceTopic,
		TargetOverride: input.TargetOverride,
		Group:          input.Group,
		SinceMs:        input.SinceMs,
		FromStart:      input.FromStart,
		Max:            input.Max,
		MaxReplays:     input.MaxReplays,
		DryRun:         input.DryRun,
	})
	if err != nil {
		logger.Error("DLQ replay activity failed",
			"source_topic", input.SourceTopic,
			"error", err)
		return nil, ierr.WithError(err).
			WithHint("Failed to replay DLQ topic").
			WithReportableDetails(map[string]interface{}{
				"source_topic": input.SourceTopic,
			}).
			Mark(ierr.ErrInternal)
	}

	logger.Info("Completed DLQ replay activity",
		"source_topic", input.SourceTopic,
		"scanned", summary.Scanned,
		"replayed", summary.Replayed,
		"skipped", summary.Skipped,
		"quarantined", summary.Quarantined)

	return &models.ReplayDLQWorkflowResult{
		Scanned:     summary.Scanned,
		Replayed:    summary.Replayed,
		Skipped:     summary.Skipped,
		Quarantined: summary.Quarantined,
		ByTarget:    summary.ByTarget,
		ByReason:    summary.ByReason,
	}, nil
}
