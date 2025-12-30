package subscription

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/service"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

const ActivityPrefix = "SubscriptionActivities"

const (
	// Workflow name - must match the function name
	WorkflowProcessSubscriptionBilling = "ProcessSubscriptionBillingWorkflow"
)

// SubscriptionActivities contains all subscription-related activities
// When registered with Temporal, methods will be called as "SubscriptionActivities.ScheduleBillingActivity"
type SubscriptionActivities struct {
	subscriptionService service.SubscriptionService
}

// NewSubscriptionActivities creates a new SubscriptionActivities instance
func NewSubscriptionActivities(subscriptionService service.SubscriptionService) *SubscriptionActivities {
	return &SubscriptionActivities{
		subscriptionService: subscriptionService,
	}
}

// ScheduleBillingActivity schedules billing workflows for subscriptions
// This method will be registered as "ScheduleBillingActivity" in Temporal
func (s *SubscriptionActivities) ScheduleBillingActivity(ctx context.Context, input subscriptionModels.ScheduleSubscriptionBillingWorkflowInput) (*subscriptionModels.ScheduleSubscriptionBillingWorkflowResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	response := &subscriptionModels.ScheduleSubscriptionBillingWorkflowResult{
		SubscriptionIDs: make([]string, 0),
	}

	offset := 0
	filter := &types.SubscriptionFilter{
		QueryFilter: &types.QueryFilter{
			Limit:  lo.ToPtr(input.BatchSize),
			Offset: lo.ToPtr(offset),
			Status: lo.ToPtr(types.StatusPublished),
		},
		SubscriptionStatus: []types.SubscriptionStatus{types.SubscriptionStatusActive},
		TimeRangeFilter: &types.TimeRangeFilter{
			EndTime: &now,
		},
	}

	subs, err := s.subscriptionService.ListAllTenantSubscriptions(ctx, filter)
	if err != nil {
		return response, err
	}

	temporalSvc := temporalService.GetGlobalTemporalService()
	subItems := subs.Items
	for _, sub := range subItems {
		// update context to include the tenant id
		ctx = context.WithValue(ctx, types.CtxTenantID, sub.TenantID)
		ctx = context.WithValue(ctx, types.CtxEnvironmentID, sub.EnvironmentID)
		ctx = context.WithValue(ctx, types.CtxUserID, sub.CreatedBy)

		// Here we need to launch a new workflow to update the billing period

		_, err := temporalSvc.ExecuteWorkflow(
			ctx,
			WorkflowProcessSubscriptionBilling,
			subscriptionModels.ProcessSubscriptionBillingWorkflowInput{
				SubscriptionID: sub.ID,
				TenantID:       sub.TenantID,
				EnvironmentID:  sub.EnvironmentID,
				UserID:         sub.CreatedBy,
				PeriodStart:    sub.CurrentPeriodStart,
				PeriodEnd:      sub.CurrentPeriodEnd,
			},
		)
		if err != nil {
			return response, err
		}
		response.SubscriptionIDs = append(response.SubscriptionIDs, sub.ID)
	}

	return response, nil
}
