package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityProcessRenewalDueAlerts = "ProcessRenewalDueAlertsActivity"
)

var temporalScheduledStartTimeKey = temporal.NewSearchAttributeKeyTime("TemporalScheduledStartTime")

// SubscriptionRenewalDueAlertsWorkflow sends renewal-due webhooks; same as POST /v1/cron/subscriptions/renewal-due-alerts.
func SubscriptionRenewalDueAlertsWorkflow(ctx workflow.Context, _ cronModels.SubscriptionRenewalDueAlertsWorkflowInput) (*cronModels.SubscriptionRenewalDueAlertsWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting SubscriptionRenewalDueAlertsWorkflow")

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

	var runTime time.Time
	scheduledTime, ok := workflow.GetTypedSearchAttributes(ctx).GetTime(temporalScheduledStartTimeKey)
	if ok && !scheduledTime.IsZero() {
		runTime = scheduledTime
	} else {
		runTime = workflow.Now(ctx)
	}
	log.Info("Using reference time for renewal-due alerts", "run_time", runTime)

	var result cronModels.SubscriptionRenewalDueAlertsWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityProcessRenewalDueAlerts, runTime).Get(ctx, &result); err != nil {
		log.Error("SubscriptionRenewalDueAlertsWorkflow activity failed", "error", err)
		return nil, err
	}
	return &result, nil
}
