package invoice

import (
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowFinalizeDraftInvoice = "FinalizeDraftInvoiceWorkflow"
)

// FinalizeDraftInvoiceWorkflow finalizes an already-computed draft invoice.
// Unlike ProcessInvoiceWorkflow, this skips the Compute step — the invoice's
// line items and totals are already populated. Used by the scheduled draft
// finalization cron to avoid re-computing usage after the billing period closed.
//
// Steps:
//  1. Finalize (assign invoice number, seal)
//  2. Sync to external vendors
//  3. Attempt payment
func FinalizeDraftInvoiceWorkflow(
	ctx workflow.Context,
	input invoiceModels.ProcessInvoiceWorkflowInput,
) (*invoiceModels.ProcessInvoiceWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting finalize draft invoice workflow",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return nil, err
	}

	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// ================================================================================
	// STEP 1: Finalize Invoice (assign invoice number, seal)
	// ================================================================================
	logger.Info("Step 1: Finalizing invoice", "invoice_id", input.InvoiceID)

	var finalizeOutput invoiceModels.FinalizeInvoiceActivityOutput
	finalizeInput := invoiceModels.FinalizeInvoiceActivityInput{
		InvoiceID:     input.InvoiceID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        input.UserID,
	}

	err := workflow.ExecuteActivity(ctx, ActivityFinalizeInvoice, finalizeInput).Get(ctx, &finalizeOutput)
	if err != nil {
		logger.Error("Failed to finalize invoice", "error", err, "invoice_id", input.InvoiceID)
		return nil, err
	}

	if finalizeOutput.Skipped {
		logger.Info("Finalization not yet due, exiting workflow",
			"invoice_id", input.InvoiceID)
		return &invoiceModels.ProcessInvoiceWorkflowResult{
			Success:     true,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	// ================================================================================
	// STEP 2: Sync Invoice to External Vendor — DISABLED
	// FinalizeInvoice (step 1) publishes WebhookEventInvoiceUpdateFinalized.
	// The integration consumer handles fan-out to per-provider sync workflows,
	// so calling SyncInvoiceToVendorActivity here would duplicate every sync.
	// ================================================================================
	// var syncOutput invoiceModels.SyncInvoiceActivityOutput
	// syncInput := invoiceModels.SyncInvoiceActivityInput{
	// 	InvoiceID:     input.InvoiceID,
	// 	TenantID:      input.TenantID,
	// 	EnvironmentID: input.EnvironmentID,
	// 	UserID:        input.UserID,
	// }
	// err = workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToVendor, syncInput).Get(ctx, &syncOutput)
	// if err != nil {
	// 	logger.Error("Failed to sync invoice to external vendor", "error", err, "invoice_id", input.InvoiceID)
	// 	return nil, err
	// }

	// ================================================================================
	// STEP 3: Attempt Payment
	// ================================================================================
	logger.Info("Step 3: Attempting payment for invoice", "invoice_id", input.InvoiceID)

	var paymentOutput invoiceModels.PaymentActivityOutput
	paymentInput := invoiceModels.PaymentActivityInput{
		InvoiceID:     input.InvoiceID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        input.UserID,
	}

	err = workflow.ExecuteActivity(ctx, ActivityAttemptInvoicePayment, paymentInput).Get(ctx, &paymentOutput)
	if err != nil {
		logger.Error("Failed to attempt payment for invoice", "error", err, "invoice_id", input.InvoiceID)
		return nil, err
	}

	logger.Info("Successfully finalized draft invoice", "invoice_id", input.InvoiceID)

	return &invoiceModels.ProcessInvoiceWorkflowResult{
		Success:     true,
		CompletedAt: workflow.Now(ctx),
	}, nil
}
