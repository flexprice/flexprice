package subscription

import (
	"time"

	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowSandboxSubscriptionCleanup         = "SandboxSubscriptionCleanupWorkflow"
	ActivityBuildSandboxCleanupList            = "BuildSandboxCleanupListActivity"
	ActivityTerminateSandboxSubscriptionsBatch = "TerminateSandboxSubscriptionsBatchActivity"
	TerminateBatchSize                         = 500
)

// SandboxSubscriptionCleanupWorkflow runs daily: builds sandboxCleanupList (subs past cleanup window) then terminates them in batches.
// tenant_config.sandbox_subscription_expiry_days (default from types) controls the cleanup window. Schedule/workflow/activity timeouts
// are set so a full run (build list + many batches) completes; each batch has its own timeout and retry.
func SandboxSubscriptionCleanupWorkflow(ctx workflow.Context) (*subscriptionModels.SandboxSubscriptionCleanupWorkflowResult, error) {
	buildListOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 1 * time.Hour,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	buildCtx := workflow.WithActivityOptions(ctx, buildListOpts)

	var listResult subscriptionModels.BuildSandboxCleanupListResult
	if err := workflow.ExecuteActivity(buildCtx, ActivityBuildSandboxCleanupList).Get(buildCtx, &listResult); err != nil {
		return nil, err
	}

	sandboxCleanupList := listResult.SubsToTerminate
	if len(sandboxCleanupList) == 0 {
		return &subscriptionModels.SandboxSubscriptionCleanupWorkflowResult{TerminatedSubscriptionIDs: []string{}}, nil
	}

	batchOpts := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 2,
			MaximumAttempts:    3,
		},
	}
	batchCtx := workflow.WithActivityOptions(ctx, batchOpts)

	var allTerminated []string
	for i := 0; i < len(sandboxCleanupList); i += TerminateBatchSize {
		end := i + TerminateBatchSize
		if end > len(sandboxCleanupList) {
			end = len(sandboxCleanupList)
		}
		batch := sandboxCleanupList[i:end]
		var batchIDs []string
		if err := workflow.ExecuteActivity(batchCtx, ActivityTerminateSandboxSubscriptionsBatch, batch).Get(batchCtx, &batchIDs); err != nil {
			return nil, err
		}
		allTerminated = append(allTerminated, batchIDs...)
	}

	return &subscriptionModels.SandboxSubscriptionCleanupWorkflowResult{TerminatedSubscriptionIDs: allTerminated}, nil
}
