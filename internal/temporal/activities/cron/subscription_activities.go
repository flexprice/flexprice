package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/activity"
)

// SubscriptionCronActivities wraps subscription-related cron jobs: auto-cancellation, billing
// period updates, and renewal-due alerts (one SubscriptionService).
type SubscriptionCronActivities struct {
	subscriptionService service.SubscriptionService
	logger              *logger.Logger
}

// NewSubscriptionCronActivities builds activities for subscription cron workflows.
func NewSubscriptionCronActivities(subscriptionService service.SubscriptionService, log *logger.Logger) *SubscriptionCronActivities {
	return &SubscriptionCronActivities{
		subscriptionService: subscriptionService,
		logger:              log,
	}
}

// ProcessAutoCancellationActivity cancels subscriptions past their grace period.
func (a *SubscriptionCronActivities) ProcessAutoCancellationActivity(ctx context.Context) (*cronModels.SubscriptionAutoCancellationWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Processing subscription auto-cancellations")

	if err := a.subscriptionService.ProcessAutoCancellationSubscriptions(ctx); err != nil {
		return nil, err
	}

	result := &cronModels.SubscriptionAutoCancellationWorkflowResult{}
	log.Info("Completed subscription auto-cancellation processing")
	return result, nil
}

// UpdateBillingPeriodsActivity runs the same work as POST /v1/cron/subscriptions/update-periods.
func (a *SubscriptionCronActivities) UpdateBillingPeriodsActivity(ctx context.Context) (*cronModels.SubscriptionBillingPeriodsWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Updating subscription billing periods (cron activity)")
	_, err := a.subscriptionService.UpdateBillingPeriods(ctx)
	if err != nil {
		return nil, err
	}
	return &cronModels.SubscriptionBillingPeriodsWorkflowResult{}, nil
}

// ProcessTrialEndDueActivity runs the same work as POST /v1/cron/subscriptions/process-trial-end-due.
func (a *SubscriptionCronActivities) ProcessTrialEndDueActivity(ctx context.Context) (*cronModels.SubscriptionTrialEndDueWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Processing trial end due subscriptions (cron activity)")
	resp, err := a.subscriptionService.ProcessTrialEndDue(ctx)
	if err != nil {
		return nil, err
	}
	return &cronModels.SubscriptionTrialEndDueWorkflowResult{
		TotalSuccess: resp.TotalSuccess,
		TotalFailed:  resp.TotalFailed,
		StartAt:      resp.StartAt,
	}, nil
}

// ProcessRenewalDueAlertsActivity runs the same work as POST /v1/cron/subscriptions/renewal-due-alerts.
func (a *SubscriptionCronActivities) ProcessRenewalDueAlertsActivity(ctx context.Context) (*cronModels.SubscriptionRenewalDueAlertsWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Processing subscription renewal-due alerts (cron activity)")
	if err := a.subscriptionService.ProcessSubscriptionRenewalDueAlert(ctx); err != nil {
		return nil, err
	}
	return &cronModels.SubscriptionRenewalDueAlertsWorkflowResult{}, nil
}
