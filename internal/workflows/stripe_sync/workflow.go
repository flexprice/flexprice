package stripe_sync

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type SyncUsageWorkflowInput struct {
	MeterID                  string
	ExternalCustomerID       string
	StartTime                time.Time
	EndTime                  time.Time
	StripeSubscriptionItemID string
	TenantID                 string
}

func SyncUsageWorkflow(ctx workflow.Context, input SyncUsageWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting stripe usage sync workflow")

	activityOpts := workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 5,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOpts)

	// Use the registered activity's method name
	err := workflow.ExecuteActivity(ctx, "Execute", input).Get(ctx, nil)
	if err != nil {
		logger.Error("Failed to sync usage to stripe", "error", err)
		return err
	}

	return nil
}
