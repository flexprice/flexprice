package cron

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/activity"
)

// DailyDraftAndComputeActivities runs the daily draft-and-compute job.
type DailyDraftAndComputeActivities struct {
	invoiceService      service.InvoiceService
	subscriptionService service.SubscriptionService
	logger              *logger.Logger
}

// NewDailyDraftAndComputeActivities builds activities for the daily draft-and-compute cron workflow.
func NewDailyDraftAndComputeActivities(
	invoiceService service.InvoiceService,
	subscriptionService service.SubscriptionService,
	log *logger.Logger,
) *DailyDraftAndComputeActivities {
	return &DailyDraftAndComputeActivities{
		invoiceService:      invoiceService,
		subscriptionService: subscriptionService,
		logger:              log,
	}
}

// DailyDraftAndComputeActivity triggers daily invoice recomputation.
func (a *DailyDraftAndComputeActivities) DailyDraftAndComputeActivity(
	ctx context.Context,
	input cronModels.DailyDraftAndComputeActivityInput,
) (*cronModels.DailyDraftAndComputeWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting daily draft-and-compute activity", "reference_time", input.ReferenceTime)

	result := &cronModels.DailyDraftAndComputeWorkflowResult{}
	tenantEnvsSeen := make(map[string]struct{})

	subs, err := a.invoiceService.ListSubscriptionsDueForDailyDraftCompute(ctx)
	if err != nil {
		log.Error("Failed to list subscriptions due for daily draft-and-compute", "error", err)
		return nil, err
	}
	result.TotalDueSubscriptions = len(subs)

	for _, sub := range subs {
		tenantEnvKey := sub.TenantID + "|" + sub.EnvironmentID
		if _, seen := tenantEnvsSeen[tenantEnvKey]; !seen {
			tenantEnvsSeen[tenantEnvKey] = struct{}{}
			result.TenantEnvsProcessed++
		}

		subCtx := types.SetTenantID(ctx, sub.TenantID)
		subCtx = types.SetEnvironmentID(subCtx, sub.EnvironmentID)
		subCtx = types.SetUserID(subCtx, sub.CreatedBy)

		_, err := a.subscriptionService.TriggerSubscriptionDraftAndComputeWorkflowWithOptions(
			subCtx, sub.ID, interfaces.DraftAndComputeOptions{
				TaskQueue:             types.TemporalTaskQueueBilling,
				WorkflowID:            dailyDraftAndComputeWorkflowID(sub.ID, input.ReferenceTime),
				SkipIfAlreadyInvoiced: true,
			},
		)
		if err != nil {
			var alreadyStarted *serviceerror.WorkflowExecutionAlreadyStarted
			if errors.As(err, &alreadyStarted) {
				// Duplicate workflow IDs are expected on retries.
				result.SkippedCount++
				continue
			}
			log.Error("Failed to trigger daily draft-and-compute for subscription",
				"subscription_id", sub.ID, "error", err)
			result.FailedCount++
			continue
		}
		result.TriggeredCount++
	}

	log.Info("Completed daily draft-and-compute activity",
		"tenant_envs_processed", result.TenantEnvsProcessed,
		"total_due_subscriptions", result.TotalDueSubscriptions,
		"triggered", result.TriggeredCount,
		"skipped", result.SkippedCount,
		"failed", result.FailedCount)

	return result, nil
}

// dailyDraftAndComputeWorkflowID returns a deterministic daily workflow ID.
func dailyDraftAndComputeWorkflowID(subscriptionID string, referenceTime time.Time) string {
	return fmt.Sprintf("%s_%s_%s_%s",
		types.UUID_PREFIX_WORKFLOW,
		types.TemporalDraftAndComputeSubscriptionInvoiceWorkflow,
		subscriptionID,
		referenceTime.UTC().Format("20060102"),
	)
}
