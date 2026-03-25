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

// SandboxSubscriptionCleanupWorkflow runs daily: fetches sandbox subs in pages (offset/limit), filters to past cleanup window
// in the activity, and terminates each page in one batch. No single large payload; workflow never holds the full list.
// tenant_config.sandbox_subscription_expiry_days (default 90) controls the cleanup window.
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

	wflog := workflow.GetLogger(ctx)
	pageSize := TerminateBatchSize
	offset := 0
	totalTerminated := 0
	batchCount := 0

	for {
		var pageResult subscriptionModels.BuildSandboxCleanupListPageResult
		if err := workflow.ExecuteActivity(buildCtx, ActivityBuildSandboxCleanupList, subscriptionModels.BuildSandboxCleanupListInput{Offset: offset, Limit: pageSize}).Get(buildCtx, &pageResult); err != nil {
			return nil, err
		}
		if len(pageResult.Items) == 0 && !pageResult.HasMore {
			break
		}
		if len(pageResult.Items) > 0 {
			// Batch activity returns terminated subscription IDs for this batch only (also visible in Temporal UI as activity result).
			var terminatedSubscriptionIDs []string
			if err := workflow.ExecuteActivity(batchCtx, ActivityTerminateSandboxSubscriptionsBatch, pageResult.Items).Get(batchCtx, &terminatedSubscriptionIDs); err != nil {
				return nil, err
			}
			wflog.Info("Sandbox cleanup batch terminated", "batch_index", batchCount, "batch_size", len(terminatedSubscriptionIDs), "terminated_subscription_ids", terminatedSubscriptionIDs)
			totalTerminated += len(terminatedSubscriptionIDs)
			batchCount++
		}
		if !pageResult.HasMore {
			break
		}
		offset += pageSize
	}

	return &subscriptionModels.SandboxSubscriptionCleanupWorkflowResult{TerminatedCount: totalTerminated, BatchCount: batchCount}, nil
}
