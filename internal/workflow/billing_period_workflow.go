package workflow

import (
	"context"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

type BillingPeriodUpdatePayload struct {
	TenantID string                 `json:"tenant_id"`
	Metadata map[string]interface{} `json:"metadata"`
}

// UpdateBillingPeriodsWorkflow is the workflow function
func UpdateBillingPeriodsWorkflow(ctx workflow.Context, payload *BillingPeriodUpdatePayload) error {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting billing period update workflow",
		"tenant_id", payload.TenantID)

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 10,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	})

	// Execute activity
	var activityErr error
	err := workflow.ExecuteActivity(ctx, UpdateBillingPeriodsActivity, payload).Get(ctx, &activityErr)
	if err != nil {
		logger.Error("Billing period update workflow failed",
			"tenant_id", payload.TenantID,
			"error", err)
		return err
	}

	logger.Info("Billing period update workflow completed successfully",
		"tenant_id", payload.TenantID)
	return nil
}

// UpdateBillingPeriodsActivity is the activity function
func UpdateBillingPeriodsActivity(ctx context.Context, payload *BillingPeriodUpdatePayload) error {
	logger := activity.GetLogger(ctx)
	logger.Info("Starting billing period update activity",
		"tenant_id", payload.TenantID)
	return nil
}
