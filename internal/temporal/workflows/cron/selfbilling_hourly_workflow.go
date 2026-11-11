package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivitySelfBillingHourly = "SelfBillingHourlyActivity"
)

// SelfBillingHourlyWorkflow runs self-billing's hourly usage push and status sync.
// Retries are safe: usage events carry deterministic IDs and the status sync is idempotent.
func SelfBillingHourlyWorkflow(ctx workflow.Context, _ cronModels.SelfBillingHourlyWorkflowInput) error {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	return workflow.ExecuteActivity(ctx, ActivitySelfBillingHourly).Get(ctx, nil)
}
