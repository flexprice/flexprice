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

	var result cronModels.SubscriptionRenewalDueAlertsWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityProcessRenewalDueAlerts).Get(ctx, &result); err != nil {
		log.Error("SubscriptionRenewalDueAlertsWorkflow activity failed", "error", err)
		return nil, err
	}
	return &result, nil
}
