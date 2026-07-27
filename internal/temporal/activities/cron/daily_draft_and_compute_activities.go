package cron

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
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
				SkipIfAlreadyInvoiced: true,
			},
		)
		if err != nil {
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
		"failed", result.FailedCount)

	if result.FailedCount > 0 {
		return nil, fmt.Errorf("failed to trigger %d daily draft-and-compute workflows", result.FailedCount)
	}

	return result, nil
}
