package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityExpireCheckoutSessions = "ExpireCheckoutSessionsActivity"
)

// CheckoutSessionExpiryWorkflow expires checkout sessions that have passed their expiry date.
// It is triggered by a Temporal Schedule every 30 minutes.
func CheckoutSessionExpiryWorkflow(ctx workflow.Context, _ cronModels.CheckoutSessionExpiryWorkflowInput) (*cronModels.CheckoutSessionExpiryWorkflowResult, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.CheckoutSessionExpiryWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityExpireCheckoutSessions).Get(ctx, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
