package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityRecognizeRevenue = "RecognizeRevenueActivity"
	ActivityBackfillRevenue  = "BackfillRevenueActivity"

	// recognitionBillingLookback re-posts billing changed in the last week, so a post that failed is retried by later runs.
	recognitionBillingLookback = 7 * 24 * time.Hour
)

// RevenueRecognitionWorkflow posts missed billing and recognizes revenue as of its scheduled time.
func RevenueRecognitionWorkflow(ctx workflow.Context, in cronModels.RevenueRecognitionInput) error {
	asOf, ok := scheduledStartTime(ctx)
	if !ok {
		asOf = workflow.Now(ctx)
	}
	since := asOf.Add(-recognitionBillingLookback)
	if in.Since != nil {
		since = *in.Since
	}

	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// The run heartbeats per contract and resumes from its cursor on retry.
		StartToCloseTimeout: 2 * time.Hour,
		HeartbeatTimeout:    10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	})

	return workflow.ExecuteActivity(ctx, ActivityRecognizeRevenue, cronModels.RecognizeRevenueActivityInput{Since: since, AsOf: asOf}).Get(ctx, nil)
}

// RevenueRecognitionBackfillWorkflow rebuilds one environment's revenue recognition from history, as of its start.
func RevenueRecognitionBackfillWorkflow(ctx workflow.Context, in cronModels.RevenueRecognitionBackfillInput) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		// A full history can be large; the run heartbeats per contract and resumes from its cursor on retry.
		StartToCloseTimeout: 12 * time.Hour,
		HeartbeatTimeout:    10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	})

	return workflow.ExecuteActivity(ctx, ActivityBackfillRevenue, cronModels.BackfillRevenueActivityInput{
		TenantID:      in.TenantID,
		EnvironmentID: in.EnvironmentID,
		AsOf:          workflow.Now(ctx),
	}).Get(ctx, nil)
}
