package invoice

import (
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowProcessInvoice = "ProcessInvoiceWorkflow"
	// Activity names - must match the registered method names
	ActivityComputeInvoice         = "ComputeInvoiceActivity"
	ActivityFinalizeInvoice        = "FinalizeInvoiceActivity"
	ActivitySyncInvoiceToVendor    = "SyncInvoiceToVendorActivity"
	ActivityAttemptInvoicePayment  = "AttemptInvoicePaymentActivity"
	ActivityTriggerInvoiceWorkflow = "TriggerInvoiceWorkflowActivity"
)

// ProcessInvoiceWorkflow processes a single invoice
// This workflow orchestrates invoice processing:
// 0. Compute invoice (line items, coupons/taxes, or mark SKIPPED if zero-dollar)
// 1. Finalize the invoice (assigns invoice number, skipped if SKIPPED)
// 2. Sync invoice to external vendors
// 3. Attempt payment for the invoice
func ProcessInvoiceWorkflow(
	ctx workflow.Context,
	input invoiceModels.ProcessInvoiceWorkflowInput,
) (*invoiceModels.ProcessInvoiceWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting process invoice workflow",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Validate input
	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return nil, err
	}

	// Define activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// ================================================================================
	// STEP 0: Compute invoice (line items, coupons/taxes, or mark SKIPPED if zero)
	// ================================================================================
	logger.Info("Step 0: Computing invoice",
		"invoice_id", input.InvoiceID)

	var computeOutput invoiceModels.ComputeInvoiceActivityOutput
	computeInput := invoiceModels.ComputeInvoiceActivityInput{
		InvoiceID:     input.InvoiceID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		UserID:        input.UserID,
	}

	err := workflow.ExecuteActivity(ctx, ActivityComputeInvoice, computeInput).Get(ctx, &computeOutput)
	if err != nil {
		logger.Error("Failed to compute invoice",
			"error", err,
			"invoice_id", input.InvoiceID)
		return nil, err
	}

	if computeOutput.Skipped {
		logger.Info("Invoice is zero-dollar, marked SKIPPED; skipping finalize/sync/payment",
			"invoice_id", input.InvoiceID)
		return &invoiceModels.ProcessInvoiceWorkflowResult{
			Success:     true,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	// ================================================================================
	// STEP 1: Finalize Invoice
	// ================================================================================
	logger.Info("Step 1: Finalizing invoice",
		"invoice_id", input.InvoiceID)

	var finalizeOutput invoiceModels.FinalizeInvoiceActivityOutput
	finalizeInput := invoiceModels.FinalizeInvoiceActivityInput{
		InvoiceID:     input.InvoiceID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
	}

	err = workflow.ExecuteActivity(ctx, ActivityFinalizeInvoice, finalizeInput).Get(ctx, &finalizeOutput)
	if err != nil {
		logger.Error("Failed to finalize invoice",
			"error", err,
			"invoice_id", input.InvoiceID)
		return nil, err
	}

	if finalizeOutput.Skipped {
		logger.Info("Finalization not yet due, exiting workflow; cron will finalize later",
			"invoice_id", input.InvoiceID)
		return &invoiceModels.ProcessInvoiceWorkflowResult{
			Success:     true,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	// ================================================================================
	// STEP 2: Sync Invoice to External Vendor — DISABLED
	// FinalizeInvoice publishes WebhookEventInvoiceUpdateFinalized to system_events.
	// The integration consumer handles the fan-out to per-provider sync workflows
	// asynchronously, so this activity is redundant and would cause duplicate syncs.
	// ================================================================================
	// var syncOutput invoiceModels.SyncInvoiceActivityOutput
	// syncInput := invoiceModels.SyncInvoiceActivityInput{
	// 	InvoiceID:     input.InvoiceID,
	// 	TenantID:      input.TenantID,
	// 	EnvironmentID: input.EnvironmentID,
	// }
	// err = workflow.ExecuteActivity(ctx, ActivitySyncInvoiceToVendor, syncInput).Get(ctx, &syncOutput)
	// if err != nil {
	// 	logger.Error("Failed to sync invoice to external vendor",
	// 		"error", err,
	// 		"invoice_id", input.InvoiceID)
	// 	return nil, err
	// }

	// ================================================================================
	// STEP 3: Attempt Payment
	// ================================================================================
	logger.Info("Step 3: Attempting payment for invoice",
		"invoice_id", input.InvoiceID)

	var paymentOutput invoiceModels.PaymentActivityOutput
	paymentInput := invoiceModels.PaymentActivityInput{
		InvoiceID:     input.InvoiceID,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
	}

	err = workflow.ExecuteActivity(ctx, ActivityAttemptInvoicePayment, paymentInput).Get(ctx, &paymentOutput)
	if err != nil {
		logger.Error("Failed to attempt payment for invoice",
			"error", err,
			"invoice_id", input.InvoiceID)
		return nil, err
	}

	logger.Info("Successfully processed invoice",
		"invoice_id", input.InvoiceID)

	return &invoiceModels.ProcessInvoiceWorkflowResult{
		Success:     true,
		CompletedAt: workflow.Now(ctx),
	}, nil
}
