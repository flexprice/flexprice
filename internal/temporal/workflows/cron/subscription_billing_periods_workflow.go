package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityUpdateBillingPeriods = "UpdateBillingPeriodsActivity"
)

// SubscriptionBillingPeriodsWorkflow updates active subscription billing periods.
// Same work as POST /v1/cron/subscriptions/update-periods; run via Temporal Schedule.
func SubscriptionBillingPeriodsWorkflow(ctx workflow.Context, _ cronModels.SubscriptionBillingPeriodsWorkflowInput) (*cronModels.SubscriptionBillingPeriodsWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting SubscriptionBillingPeriodsWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.SubscriptionBillingPeriodsWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityUpdateBillingPeriods).Get(ctx, &result); err != nil {
		log.Error("SubscriptionBillingPeriodsWorkflow activity failed", "error", err)
		return nil, err
	}
	return &result, nil
}
