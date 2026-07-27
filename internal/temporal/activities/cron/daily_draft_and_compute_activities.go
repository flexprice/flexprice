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

// heartbeatEvery controls how often DailyDraftAndComputeActivity records a Temporal heartbeat
// while triggering subscriptions — matches ExpireCreditsActivity's cadence for the same
// cross-tenant fan-out shape (see wallet_activities.go).
const heartbeatEvery = 100

// DailyDraftAndComputeActivities wraps the daily draft-and-compute cron job (one InvoiceService,
// one SubscriptionService — both already Temporal-agnostic).
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

// DailyDraftAndComputeActivity finds every active subscription in a tenant×environment where
// draft_invoice_recompute_config.enabled is true, and triggers
// DraftAndComputeSubscriptionInvoiceWorkflow for each on the dedicated "billing" task queue with
// a day-stamped, deterministic workflow ID. Never finalizes anything.
func (a *DailyDraftAndComputeActivities) DailyDraftAndComputeActivity(
	ctx context.Context,
	input cronModels.DailyDraftAndComputeActivityInput,
) (*cronModels.DailyDraftAndComputeWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting daily draft-and-compute activity", "reference_time", input.ReferenceTime)

	result := &cronModels.DailyDraftAndComputeWorkflowResult{}
	tenantEnvsSeen := make(map[string]struct{})

	subs, err := a.invoiceService.ListSubscriptionsDueForDailyDraftCompute(ctx, func() {
		activity.RecordHeartbeat(ctx, "scanning tenant environments")
	})
	if err != nil {
		log.Error("Failed to list subscriptions due for daily draft-and-compute", "error", err)
		return nil, err
	}
	result.TotalDueSubscriptions = len(subs)

	for i, sub := range subs {
		if i%heartbeatEvery == 0 {
			activity.RecordHeartbeat(ctx, fmt.Sprintf("triggered %d/%d", i, len(subs)))
		}

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
				// Expected on any retry: this subscription was already triggered (or already
				// completed, per WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE) earlier in this
				// same activity attempt. Not a failure — routine retries must not look like
				// production incidents.
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

// dailyDraftAndComputeWorkflowID returns the deterministic, day-stamped workflow ID used to dedupe
// a subscription's daily draft-and-compute trigger for a given reference time. Pure function —
// referenceTime must be a fixed value threaded down from the parent workflow (see
// DailyDraftAndComputeActivityInput.ReferenceTime), never time.Now(), or a retry hours later could
// stamp a different day and silently defeat the dedupe.
func dailyDraftAndComputeWorkflowID(subscriptionID string, referenceTime time.Time) string {
	return fmt.Sprintf("%s_%s_%s_%s",
		types.UUID_PREFIX_WORKFLOW,
		types.TemporalDraftAndComputeSubscriptionInvoiceWorkflow,
		subscriptionID,
		referenceTime.UTC().Format("20060102"),
	)
}
