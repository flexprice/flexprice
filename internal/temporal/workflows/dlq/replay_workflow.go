package dlq

import (
	"time"

	models "github.com/flexprice/flexprice/internal/temporal/models/dlq"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// WorkflowReplayDLQ - must match the workflow function name.
	WorkflowReplayDLQ = "ReplayDLQWorkflow"
	// ActivityReplayDLQ - must match the registered activity method name.
	ActivityReplayDLQ = "ReplayDLQ"
)

// ReplayDLQWorkflow drains a dead-letter topic back to its origin via the
// ReplayDLQ activity. The activity is idempotent across retries because the
// replay engine resumes from committed offsets, so an activity retry continues
// the drain rather than restarting it.
func ReplayDLQWorkflow(ctx workflow.Context, input models.ReplayDLQWorkflowInput) (*models.ReplayDLQWorkflowResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting DLQ replay workflow",
		"source_topic", input.SourceTopic,
		"dry_run", input.DryRun)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 30,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result models.ReplayDLQWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityReplayDLQ, input).Get(ctx, &result); err != nil {
		logger.Error("DLQ replay workflow failed",
			"source_topic", input.SourceTopic,
			"error", err)
		return nil, err
	}

	result.CompletedAt = workflow.Now(ctx)
	logger.Info("DLQ replay workflow completed",
		"source_topic", input.SourceTopic,
		"scanned", result.Scanned,
		"replayed", result.Replayed,
		"skipped", result.Skipped,
		"quarantined", result.Quarantined)
	return &result, nil
}
