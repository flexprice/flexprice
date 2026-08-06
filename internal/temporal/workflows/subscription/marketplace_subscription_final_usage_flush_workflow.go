package subscription

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowMarketplaceSubscriptionFinalUsageFlush = "MarketplaceSubscriptionFinalUsageFlushWorkflow"
	// Activity name - must match the registered method name
	ActivityMarketplaceSubscriptionFinalUsageFlush = "MarketplaceSubscriptionFinalUsageFlushActivity"
)

// MarketplaceSubscriptionFinalUsageFlushWorkflow reports a cancelled subscription's outstanding
// marketplace usage and archives its marketplace mappings. It is started once per cancellation, after
// the cancellation commits, rather than on a schedule: AWS and GCP accept a final report for only
// about an hour afterwards, far tighter than the reporting crons' cadence.
//
// The activity archives the mappings only once everything reported successfully; on failure they stay
// published so the reporting cron can keep retrying the records.
func MarketplaceSubscriptionFinalUsageFlushWorkflow(
	ctx workflow.Context,
	input models.MarketplaceSubscriptionFinalUsageFlushWorkflowInput,
) (*models.MarketplaceSubscriptionFinalUsageFlushWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting MarketplaceSubscriptionFinalUsageFlushWorkflow", "subscription_id", input.SubscriptionID)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    1 * time.Minute,
			MaximumAttempts:    5,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result models.MarketplaceSubscriptionFinalUsageFlushWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityMarketplaceSubscriptionFinalUsageFlush, models.MarketplaceSubscriptionFinalUsageFlushActivityInput{
		SubscriptionID: input.SubscriptionID,
		CancelAt:       input.CancelAt,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
	}).Get(ctx, &result); err != nil {
		log.Error("MarketplaceSubscriptionFinalUsageFlushWorkflow activity failed",
			"subscription_id", input.SubscriptionID, "error", err)
		return nil, err
	}

	log.Info("MarketplaceSubscriptionFinalUsageFlushWorkflow completed",
		"subscription_id", input.SubscriptionID,
		"final_record_id", result.FinalRecordID,
		"records_succeeded", result.RecordsSucceeded,
		"records_skipped", result.RecordsSkipped,
		"mappings_delinked", result.MappingsDelinked)
	return &result, nil
}
