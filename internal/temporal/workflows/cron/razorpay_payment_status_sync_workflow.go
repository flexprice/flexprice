package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivitySyncRazorpayInFlightPayments = "SyncRazorpayInFlightPaymentsActivity"
)

// RazorpayPaymentStatusSyncWorkflow settles in-flight Razorpay payments whose webhook never arrived.
func RazorpayPaymentStatusSyncWorkflow(ctx workflow.Context, _ struct{}) (*cronModels.RazorpayPaymentStatusSyncResult, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})

	var result cronModels.RazorpayPaymentStatusSyncResult
	if err := workflow.ExecuteActivity(ctx, ActivitySyncRazorpayInFlightPayments).Get(ctx, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
