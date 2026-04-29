package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityRetryStaleOutboundWebhooks = "RetryStaleOutboundWebhooksActivity"
)

// OutboundWebhookStaleRetryWorkflow retries system_events that were never published after a grace period.
// Triggered by a Temporal schedule every 15 minutes.
func OutboundWebhookStaleRetryWorkflow(ctx workflow.Context, _ cronModels.OutboundWebhookStaleRetryWorkflowInput) (*cronModels.OutboundWebhookStaleRetryWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting OutboundWebhookStaleRetryWorkflow")

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

	var result cronModels.OutboundWebhookStaleRetryWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityRetryStaleOutboundWebhooks).Get(ctx, &result); err != nil {
		log.Error("OutboundWebhookStaleRetryWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("OutboundWebhookStaleRetryWorkflow completed",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return &result, nil
}
