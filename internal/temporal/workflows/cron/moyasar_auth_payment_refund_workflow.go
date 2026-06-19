package cron

import (
	"time"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowMoyasarAuthPaymentRefund            = "MoyasarAuthPaymentRefundWorkflow"
	ActivityReconcilePendingAuthPayments         = "ReconcilePendingAuthPaymentsActivity"
	ActivityVoidOrRefundSucceededAuthPayments    = "VoidOrRefundSucceededAuthPaymentsActivity"
)

// MoyasarAuthPaymentRefundWorkflow is a cron workflow that:
//  1. Reconciles PENDING AUTH payments against Moyasar (Activity A)
//  2. Voids or refunds SUCCEEDED AUTH payments (Activity B)
func MoyasarAuthPaymentRefundWorkflow(ctx workflow.Context, _ struct{}) error {
	log := workflow.GetLogger(ctx)
	log.Info("Starting MoyasarAuthPaymentRefundWorkflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			MaximumAttempts: 3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	// Activity A: reconcile PENDING AUTH payments
	if err := workflow.ExecuteActivity(ctx, ActivityReconcilePendingAuthPayments).Get(ctx, nil); err != nil {
		log.Error("ReconcilePendingAuthPaymentsActivity failed", "error", err)
		return err
	}

	// Activity B: void or refund SUCCEEDED AUTH payments
	if err := workflow.ExecuteActivity(ctx, ActivityVoidOrRefundSucceededAuthPayments).Get(ctx, nil); err != nil {
		log.Error("VoidOrRefundSucceededAuthPaymentsActivity failed", "error", err)
		return err
	}

	log.Info("MoyasarAuthPaymentRefundWorkflow completed successfully")
	return nil
}
