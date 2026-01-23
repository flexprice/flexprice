package subscription

import (
	"context"

	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/service"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"go.temporal.io/sdk/activity"

	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
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
	logger := activity.GetLogger(ctx)
	response := &subscriptionModels.ScheduleSubscriptionBillingWorkflowResult{
		SubscriptionIDs: make([]string, 0),
	}

	// Validate input
	if err := input.Validate(); err != nil {
		return response, err
	}

	now := time.Now().UTC()
	offset := 0
	batchSize := input.BatchSize
	totalProcessed := 0

	// Get global temporal service once before the loop
	temporalSvc := temporalService.GetGlobalTemporalService()
	if temporalSvc == nil {
		return response, ierr.NewError("temporal service not available").
			WithHint("Temporal service not available").
			Mark(ierr.ErrInternal)
	}

	// Loop through all subscriptions with pagination
	for {
		filter := &types.SubscriptionFilter{
			QueryFilter: &types.QueryFilter{
				Limit:  lo.ToPtr(batchSize),
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
			logger.Error("Failed to list subscriptions", "offset", offset, "error", err)
			return response, err
		}

		// No more subscriptions to process
		if len(subs.Items) == 0 {
			break
		}

		logger.Info("Processing subscription batch",
			"offset", offset,
			"batch_size", len(subs.Items),
			"total_processed", totalProcessed)

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
				// Log error but continue processing other subscriptions
				logger.Error("Failed to start workflow for subscription",
					"subscription_id", sub.ID,
					"error", err)
				continue
			}
			response.SubscriptionIDs = append(response.SubscriptionIDs, sub.ID)
			totalProcessed++
		}

		// Check if we got fewer results than batch size (last page)
		if len(subs.Items) < batchSize {
			logger.Info("Reached last page of subscriptions", "total_processed", totalProcessed)
			break
		}

		// Move to next page
		offset += batchSize
	}

	logger.Info("Completed processing all subscriptions", "total_processed", totalProcessed)
	return response, nil
}
