package cron

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowMarketplaceUsageReport = "MarketplaceUsageReportWorkflow"
	ActivityMarketplaceUsageReport = "MarketplaceUsageReportActivity"
)

// MarketplaceUsageReportWorkflow reports unsynced usage records to their marketplaces. It is
// triggered by a Temporal Schedule every 3 hours. A record that is never accepted stays unsynced
// and is retried on the next scheduled run; there is no dead-letter queue or terminal state.
func MarketplaceUsageReportWorkflow(ctx workflow.Context, _ models.MarketplaceUsageReportWorkflowInput) (*models.MarketplaceUsageReportWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting MarketplaceUsageReportWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			// MaximumAttempts is left unset so activity-level retries handle transient failures
			// within a run. Per-record retries across runs are driven by the record staying
			// unsynced, not by this policy.
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result models.MarketplaceUsageReportWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityMarketplaceUsageReport, models.MarketplaceUsageReportWorkflowInput{}).Get(ctx, &result); err != nil {
		log.Error("MarketplaceUsageReportWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("MarketplaceUsageReportWorkflow completed",
		"total", result.Total,
		"succeeded", result.Succeeded,
		"failed", result.Failed,
	)
	return &result, nil
}
