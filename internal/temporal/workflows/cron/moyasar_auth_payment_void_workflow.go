package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ActivityVoidMoyasarAuthPayments = "VoidMoyasarAuthPaymentsActivity"

// MoyasarAuthPaymentVoidWorkflow voids (or refunds) SUCCEEDED Moyasar AUTH payments
// created during card tokenization. Runs every 15 minutes via a Temporal Schedule.
func MoyasarAuthPaymentVoidWorkflow(ctx workflow.Context, _ cronModels.MoyasarAuthPaymentVoidWorkflowInput) (*cronModels.MoyasarAuthPaymentVoidWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting MoyasarAuthPaymentVoidWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		HeartbeatTimeout:    2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.MoyasarAuthPaymentVoidWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityVoidMoyasarAuthPayments).Get(ctx, &result); err != nil {
		log.Error("MoyasarAuthPaymentVoidWorkflow activity failed", "error", err)
		return nil, err
	}

	log.Info("MoyasarAuthPaymentVoidWorkflow completed",
		"total", result.Total,
		"voided", result.Voided,
		"refunded", result.Refunded,
		"failed", result.Failed,
	)
	return &result, nil
}
