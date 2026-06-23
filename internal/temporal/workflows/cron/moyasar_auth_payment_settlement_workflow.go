package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowMoyasarAuthPaymentSettlement       = "MoyasarAuthPaymentSettlementWorkflow"
	ActivityReconcilePendingAuthPayments      = "ReconcilePendingAuthPaymentsActivity"
	ActivityVoidOrRefundSucceededAuthPayments = "VoidOrRefundSucceededAuthPaymentsActivity"
)

// MoyasarAuthPaymentSettlementWorkflow is a cron workflow that:
//  1. Reconciles PENDING CUSTOMER payments against Moyasar (Activity A)
//  2. Voids or refunds SUCCEEDED CUSTOMER payments (Activity B)
func MoyasarAuthPaymentSettlementWorkflow(ctx workflow.Context, _ struct{}) (*cronModels.MoyasarAuthPaymentSettlementWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	log.Info("Starting MoyasarAuthPaymentSettlementWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	result := &cronModels.MoyasarAuthPaymentSettlementWorkflowResult{}

	// Activity A: reconcile PENDING CUSTOMER payments
	if err := workflow.ExecuteActivity(ctx, ActivityReconcilePendingAuthPayments).Get(ctx, &result.Reconcile); err != nil {
		log.Error("ReconcilePendingAuthPaymentsActivity failed", "error", err)
		return nil, err
	}

	// Activity B: void or refund SUCCEEDED CUSTOMER payments
	if err := workflow.ExecuteActivity(ctx, ActivityVoidOrRefundSucceededAuthPayments).Get(ctx, &result.VoidRefund); err != nil {
		log.Error("VoidOrRefundSucceededAuthPaymentsActivity failed", "error", err)
		return nil, err
	}

	log.Info("MoyasarAuthPaymentSettlementWorkflow completed successfully")
	return result, nil
}
