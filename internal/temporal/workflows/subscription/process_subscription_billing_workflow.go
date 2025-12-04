package subscription

import (
	"time"

	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow name - must match the function name
	WorkflowProcessSubscriptionBilling = "ProcessSubscriptionBillingWorkflow"
	// Activity names - must match the registered method names
	ActivityCheckPause          = "CheckPauseActivity"
	ActivityCalculatePeriods    = "CalculatePeriodsActivity"
	ActivityProcessPeriods      = "ProcessPeriodsActivity"
	ActivityUpdateCurrentPeriod = "UpdateCurrentPeriodActivity"
	ActivityCheckCancellation   = "CheckCancellationActivity"
	ActivitySyncInvoice         = "SyncInvoiceActivity"
	ActivityAttemptPayment      = "AttemptPaymentActivity"
)

// ProcessSubscriptionBillingWorkflow processes a subscription billing workflow
// This workflow orchestrates the complete billing period processing:
// 1. Check pause status and handle pause/resume
// 2. Calculate billing periods up to current time
// 3. Process periods (create invoices for all except the last one)
// 4. Update subscription to new current period
// 5. Check for cancellation
// 6. Sync invoices to external vendors (non-blocking)
// 7. Attempt payment for invoices
func ProcessSubscriptionBillingWorkflow(
	ctx workflow.Context,
	input subscriptionModels.ProcessSubscriptionBillingWorkflowInput,
) (*subscriptionModels.ProcessSubscriptionBillingWorkflowResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting process subscription billing workflow",
		"subscription_id", input.SubscriptionID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Validate input
	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		errorMsg := err.Error()
		return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
			Success:     false,
			Error:       &errorMsg,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	// Define activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Get current time in workflow
	now := workflow.Now(ctx)

	// ================================================================================
	// STEP 1: Check Subscription Pause Status
	// ================================================================================
	logger.Info("Step 1: Checking subscription pause status",
		"subscription_id", input.SubscriptionID)

	var pauseStatusOutput subscriptionModels.CheckSubscriptionPauseStatusActivityOutput
	pauseStatusInput := subscriptionModels.CheckSubscriptionPauseStatusActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		CurrentTime:    now,
	}

	err := workflow.ExecuteActivity(ctx, ActivityCheckPause, pauseStatusInput).Get(ctx, &pauseStatusOutput)
	if err != nil {
		logger.Error("Failed to check subscription pause status",
			"error", err,
			"subscription_id", input.SubscriptionID)
		errorMsg := err.Error()
		return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
			Success:     false,
			Error:       &errorMsg,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	// If we should skip processing, return early
	if pauseStatusOutput.ShouldSkipProcessing {
		logger.Info("Skipping period processing",
			"subscription_id", input.SubscriptionID,
			"is_paused", pauseStatusOutput.IsPaused,
			"was_resumed", pauseStatusOutput.WasResumed)
		return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
			Success:     true,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	// ================================================================================
	// STEP 2: Calculate Billing Periods
	// ================================================================================
	logger.Info("Step 2: Calculating billing periods",
		"subscription_id", input.SubscriptionID,
		"current_time", now)

	var periodsOutput subscriptionModels.CalculatePeriodsActivityOutput
	calculatePeriodsInput := subscriptionModels.CalculatePeriodsActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		CurrentTime:    now,
	}

	err = workflow.ExecuteActivity(ctx, ActivityCalculatePeriods, calculatePeriodsInput).Get(ctx, &periodsOutput)
	if err != nil {
		logger.Error("Failed to calculate periods",
			"error", err,
			"subscription_id", input.SubscriptionID)
		errorMsg := err.Error()
		return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
			Success:     false,
			Error:       &errorMsg,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	// If only one period, no processing needed
	if len(periodsOutput.Periods) == 1 {
		logger.Info("No period transitions needed",
			"subscription_id", input.SubscriptionID,
			"periods_count", len(periodsOutput.Periods))
		return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
			Success:     true,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	logger.Info("Calculated billing periods",
		"subscription_id", input.SubscriptionID,
		"periods_count", len(periodsOutput.Periods),
		"has_more_periods", periodsOutput.HasMorePeriods,
		"reached_end_date", periodsOutput.ReachedEndDate)

	// ================================================================================
	// STEP 3: Process Periods (Create Invoices)
	// ================================================================================
	logger.Info("Step 3: Processing periods and creating invoices",
		"subscription_id", input.SubscriptionID,
		"periods_to_process", len(periodsOutput.Periods)-1)

	var processPeriodsOutput subscriptionModels.ProcessPeriodsActivityOutput
	processPeriodsInput := subscriptionModels.ProcessPeriodsActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		Periods:        periodsOutput.Periods,
	}

	err = workflow.ExecuteActivity(ctx, ActivityProcessPeriods, processPeriodsInput).Get(ctx, &processPeriodsOutput)
	if err != nil {
		logger.Error("Failed to process periods",
			"error", err,
			"subscription_id", input.SubscriptionID)
		errorMsg := err.Error()
		return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
			Success:     false,
			Error:       &errorMsg,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	logger.Info("Processed periods and created invoices",
		"subscription_id", input.SubscriptionID,
		"periods_processed", processPeriodsOutput.PeriodsProcessed,
		"invoices_created", len(processPeriodsOutput.InvoiceIDs))

	// ================================================================================
	// STEP 4: Update Subscription Period
	// ================================================================================
	// Update to the new current period (last period from calculated periods)
	newPeriod := periodsOutput.Periods[len(periodsOutput.Periods)-1]
	logger.Info("Step 4: Updating subscription period",
		"subscription_id", input.SubscriptionID,
		"new_period_start", newPeriod.Start,
		"new_period_end", newPeriod.End)

	var updatePeriodOutput subscriptionModels.UpdateSubscriptionPeriodActivityOutput
	updatePeriodInput := subscriptionModels.UpdateSubscriptionPeriodActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		PeriodStart:    newPeriod.Start,
		PeriodEnd:      newPeriod.End,
	}

	err = workflow.ExecuteActivity(ctx, ActivityUpdateCurrentPeriod, updatePeriodInput).Get(ctx, &updatePeriodOutput)
	if err != nil {
		logger.Error("Failed to update subscription period",
			"error", err,
			"subscription_id", input.SubscriptionID)
		errorMsg := err.Error()
		return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
			Success:     false,
			Error:       &errorMsg,
			CompletedAt: workflow.Now(ctx),
		}, nil
	}

	// ================================================================================
	// STEP 5: Check Subscription Cancellation
	// ================================================================================
	logger.Info("Step 5: Checking subscription cancellation",
		"subscription_id", input.SubscriptionID,
		"period_end", newPeriod.End)

	var cancellationOutput subscriptionModels.CheckSubscriptionCancellationActivityOutput
	cancellationInput := subscriptionModels.CheckSubscriptionCancellationActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		PeriodEnd:      newPeriod.End,
	}

	err = workflow.ExecuteActivity(ctx, ActivityCheckCancellation, cancellationInput).Get(ctx, &cancellationOutput)
	if err != nil {
		logger.Error("Failed to check subscription cancellation",
			"error", err,
			"subscription_id", input.SubscriptionID)
		// Don't fail the workflow for this, just log the error
	} else if cancellationOutput.ShouldCancel {
		logger.Info("Subscription should be cancelled",
			"subscription_id", input.SubscriptionID,
			"cancelled_at", cancellationOutput.CancelledAt)
		// Note: Actual cancellation is handled in the activity
	}

	// ================================================================================
	// STEP 6 & 7: Sync Invoices to External Vendors and Attempt Payment
	// ================================================================================
	// Process invoices in parallel (non-blocking for sync, blocking for payment)
	// Use shorter timeout for sync operations as they're non-critical
	syncActivityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 5,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 2,
			MaximumAttempts:    2, // Fewer retries for non-critical operations
		},
	}
	syncCtx := workflow.WithActivityOptions(ctx, syncActivityOptions)

	// Process each invoice
	for _, invoiceID := range processPeriodsOutput.InvoiceIDs {
		logger.Info("Processing invoice",
			"subscription_id", input.SubscriptionID,
			"invoice_id", invoiceID)

		// Step 6: Sync to external vendor (non-blocking)
		syncInput := subscriptionModels.SyncInvoiceToExternalVendorActivityInput{
			InvoiceID:      invoiceID,
			SubscriptionID: input.SubscriptionID,
			TenantID:       input.TenantID,
			EnvironmentID:  input.EnvironmentID,
		}

		// Execute sync asynchronously - don't block on errors
		_ = workflow.ExecuteActivity(syncCtx, ActivitySyncInvoice, syncInput).Get(syncCtx, nil)
		// Errors are logged in the activity, we continue regardless

		// Step 7: Attempt payment (blocking)
		paymentInput := subscriptionModels.AttemptPaymentActivityInput{
			InvoiceID:      invoiceID,
			SubscriptionID: input.SubscriptionID,
			TenantID:       input.TenantID,
			EnvironmentID:  input.EnvironmentID,
		}

		err = workflow.ExecuteActivity(ctx, ActivityAttemptPayment, paymentInput).Get(ctx, nil)
		if err != nil {
			logger.Error("Failed to attempt payment",
				"error", err,
				"subscription_id", input.SubscriptionID,
				"invoice_id", invoiceID)
			// Don't fail the entire workflow for payment failures
			// Payment can be retried later
		} else {
			logger.Info("Payment attempt completed",
				"subscription_id", input.SubscriptionID,
				"invoice_id", invoiceID)
		}
	}

	logger.Info("Process subscription billing workflow completed successfully",
		"subscription_id", input.SubscriptionID,
		"periods_processed", processPeriodsOutput.PeriodsProcessed,
		"invoices_created", len(processPeriodsOutput.InvoiceIDs))

	return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
		Success:     true,
		CompletedAt: workflow.Now(ctx),
	}, nil
}
