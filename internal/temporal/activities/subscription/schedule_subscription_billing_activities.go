package subscription

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/service"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.temporal.io/sdk/activity"

	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
)

const (
	ActivityPrefix                     = "SubscriptionActivities"
	WorkflowProcessSubscriptionBilling = "ProcessSubscriptionBillingWorkflow"
)

type SubscriptionActivities struct {
	subscriptionService service.SubscriptionService
}

func NewSubscriptionActivities(subscriptionService service.SubscriptionService) *SubscriptionActivities {
	return &SubscriptionActivities{subscriptionService: subscriptionService}
}

// ScheduleBillingActivity fetches active subscriptions in batches and starts a billing
// workflow for each. Processing stops when MaxWorkflows is reached; the next cron run
// picks up remaining subscriptions.
func (a *SubscriptionActivities) ScheduleBillingActivity(
	ctx context.Context,
	input subscriptionModels.ScheduleSubscriptionBillingWorkflowInput,
) (*subscriptionModels.ScheduleSubscriptionBillingWorkflowResult, error) {
	logger := activity.GetLogger(ctx)

	if err := input.Validate(); err != nil {
		return &subscriptionModels.ScheduleSubscriptionBillingWorkflowResult{}, err
	}

	temporalSvc := temporalService.GetGlobalTemporalService()
	now := time.Now().UTC()

	var scheduledIDs []string
	offset := 0

	for len(scheduledIDs) < input.MaxWorkflows {
		// Fetch next batch of subscriptions.
		subs, err := a.listActiveSubscriptions(ctx, now, input.BatchSize, offset)
		if err != nil {
			logger.Error("Failed to list subscriptions", "offset", offset, "error", err)
			return &subscriptionModels.ScheduleSubscriptionBillingWorkflowResult{SubscriptionIDs: scheduledIDs}, err
		}
		if len(subs) == 0 {
			break
		}

		logger.Info("Processing batch", "offset", offset, "batch_size", len(subs), "scheduled_so_far", len(scheduledIDs))

		// Start workflows for each subscription in this batch.
		for _, sub := range subs {
			if len(scheduledIDs) >= input.MaxWorkflows {
				break
			}

			workflowCtx := a.buildWorkflowContext(ctx, sub)
			workflowInput := subscriptionModels.ProcessSubscriptionBillingWorkflowInput{
				SubscriptionID: sub.ID,
				TenantID:       sub.TenantID,
				EnvironmentID:  sub.EnvironmentID,
				UserID:         sub.CreatedBy,
				PeriodStart:    sub.CurrentPeriodStart,
				PeriodEnd:      sub.CurrentPeriodEnd,
			}

			if _, err := temporalSvc.ExecuteWorkflow(workflowCtx, WorkflowProcessSubscriptionBilling, workflowInput); err != nil {
				logger.Error("Failed to start workflow", "subscription_id", sub.ID, "error", err)
				continue
			}
			scheduledIDs = append(scheduledIDs, sub.ID)
		}

		// Exit if this was the last page.
		if len(subs) < input.BatchSize {
			break
		}
		offset += input.BatchSize
	}

	// Log completion summary.
	if len(scheduledIDs) >= input.MaxWorkflows {
		logger.Info("Hit workflow cap; remaining subscriptions deferred to next cron", "scheduled", len(scheduledIDs), "max", input.MaxWorkflows)
	} else {
		logger.Info("Processed all eligible subscriptions", "scheduled", len(scheduledIDs))
	}

	return &subscriptionModels.ScheduleSubscriptionBillingWorkflowResult{SubscriptionIDs: scheduledIDs}, nil
}

// listActiveSubscriptions returns a page of active subscriptions whose current billing period has ended.
func (a *SubscriptionActivities) listActiveSubscriptions(ctx context.Context, now time.Time, limit, offset int) ([]*dto.SubscriptionResponse, error) {
	filter := &types.SubscriptionFilter{
		QueryFilter: &types.QueryFilter{
			Limit:  lo.ToPtr(limit),
			Offset: lo.ToPtr(offset),
			Status: lo.ToPtr(types.StatusPublished),
		},
		SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
		TimeRangeFilter:    &types.TimeRangeFilter{EndTime: &now},
	}
	resp, err := a.subscriptionService.ListAllTenantSubscriptions(ctx, filter)
	if err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// buildWorkflowContext attaches tenant, environment, and user IDs to the context.
func (a *SubscriptionActivities) buildWorkflowContext(ctx context.Context, sub *dto.SubscriptionResponse) context.Context {
	ctx = context.WithValue(ctx, types.CtxTenantID, sub.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, sub.EnvironmentID)
	ctx = context.WithValue(ctx, types.CtxUserID, sub.CreatedBy)
	return ctx
}
