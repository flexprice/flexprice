package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowRazorpayReconciliationSweep = "RazorpayReconciliationSweepWorkflow"
	ActivityResolveStuckClaims          = "ResolveStuckClaimsActivity"
)

// RazorpayReconciliationSweepWorkflow is a cron workflow that implements
// design spec §8's reconciliation sweep: it resolves Razorpay autocharge
// idempotency claims (InvoiceCharge/TokenCycleCharge) that have been stuck in
// "claimed" for too long, via Razorpay's read-only Payment.Fetch. It is
// triggered by a Temporal Schedule every 20 minutes. This is the ONLY place a
// stuck claim resolves — see internal/ee/service/invoice.go's
// AutoChargeInvoice, which deliberately leaves ambiguous claims "claimed" for
// this sweep to pick up.
func RazorpayReconciliationSweepWorkflow(ctx workflow.Context, _ cronModels.RazorpayReconciliationSweepWorkflowInput) (*cronModels.RazorpayReconciliationSweepWorkflowResult, error) {
	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result cronModels.RazorpayReconciliationSweepWorkflowResult
	if err := workflow.ExecuteActivity(ctx, ActivityResolveStuckClaims).Get(ctx, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
