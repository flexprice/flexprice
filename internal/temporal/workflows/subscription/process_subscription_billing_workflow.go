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
	ActivityCheckDraftSubscription = "CheckDraftSubscriptionActivity"
	ActivityCheckPause             = "CheckPauseActivity"
	ActivityCalculatePeriods       = "CalculatePeriodsActivity"
	ActivityCreateInvoices         = "CreateInvoicesActivity"
	ActivityUpdateCurrentPeriod    = "UpdateCurrentPeriodActivity"
	ActivityCheckCancellation      = "CheckCancellationActivity"
	ActivitySyncInvoice            = "SyncInvoiceActivity"
	ActivityAttemptPayment         = "AttemptPaymentActivity"
)

// ProcessSubscriptionBillingWorkflow processes a subscription billing workflow
// This workflow orchestrates the complete billing period processing:
// 1. Check pause status and handle pause/resume
// 2. Calculate billing periods up to current time
// 3. For each period (except the last), create invoice and check for cancellation
// 4. Update subscription to new current period
// 5. Check for final cancellation
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
		return nil, err
	}

	// Define activity options
	activityOptions := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    1,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, activityOptions)

	// Get current time in workflow
	now := workflow.Now(ctx)

	// ================================================================================
	// STEP 1: Check if subscription is draft
	// ================================================================================

	logger.Info("Step 1: Checking if subscription is draft",
		"subscription_id", input.SubscriptionID)

	var draftSubscriptionOutput subscriptionModels.CheckDraftSubscriptionActivityOutput
	draftSubscriptionInput := subscriptionModels.CheckDraftSubscriptionActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
	}
	err := workflow.ExecuteActivity(ctx, ActivityCheckDraftSubscription, draftSubscriptionInput).Get(ctx, &draftSubscriptionOutput)
	if err != nil {
		logger.Error("Failed to check if subscription is draft",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}

	if draftSubscriptionOutput.IsDraft {
		logger.Info("Subscription is draft, skipping period processing",
			"subscription_id", input.SubscriptionID)
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
		return nil, err
	}
	if !periodsOutput.ShouldProcess {
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
		"periods_count", len(periodsOutput.Periods))

	// ================================================================================
	// STEP 3: Process Periods (Create Invoices)
	// ================================================================================
	logger.Info("Step 3: Processing periods and creating invoices",
		"subscription_id", input.SubscriptionID,
		"periods_to_process", len(periodsOutput.Periods)-1)

	// Create invoice for completed periods (all except last)
	// The last period becomes the new current period
	completedPeriods := periodsOutput.Periods[:len(periodsOutput.Periods)-1]
	var createInvoicesOutput subscriptionModels.CreateInvoicesActivityOutput
	createInvoicesInput := subscriptionModels.CreateInvoicesActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		Periods:        completedPeriods,
	}

	err = workflow.ExecuteActivity(ctx, ActivityCreateInvoices, createInvoicesInput).Get(ctx, &createInvoicesOutput)
	if err != nil {
		logger.Error("Failed to create invoice for period",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}

	// ================================================================================
	// STEP 4: Sync Invoices to External Vendor
	// ================================================================================
	logger.Info("Step 4: Syncing invoices to external vendor",
		"subscription_id", input.SubscriptionID)

	var syncInvoicesOutput subscriptionModels.SyncInvoiceToExternalVendorActivityOutput
	syncInvoicesInput := subscriptionModels.SyncInvoiceToExternalVendorActivityInput{
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		InvoiceIDs:    createInvoicesOutput.InvoiceIDs,
	}

	err = workflow.ExecuteActivity(ctx, ActivitySyncInvoice, syncInvoicesInput).Get(ctx, &syncInvoicesOutput)
	if err != nil {
		logger.Error("Failed to sync invoice to external vendor",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}

	// ================================================================================
	// STEP 5: Attempt Payment for the invoices
	// ================================================================================

	logger.Info("Step 5: Attempting payment for invoices",
		"subscription_id", input.SubscriptionID)

	var attemptPaymentOutput subscriptionModels.AttemptPaymentActivityOutput
	attemptPaymentInput := subscriptionModels.AttemptPaymentActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		InvoiceIDs:     createInvoicesOutput.InvoiceIDs,
	}

	err = workflow.ExecuteActivity(ctx, ActivityAttemptPayment, attemptPaymentInput).Get(ctx, &attemptPaymentOutput)
	if err != nil {
		logger.Error("Failed to attempt payment for invoices",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}

	// ================================================================================
	// STEP 6: Cancellation Check
	// ================================================================================
	logger.Info("Step 6: Checking for subscription cancellation",
		"subscription_id", input.SubscriptionID)
	var cancelSubscriptionOutput subscriptionModels.CheckSubscriptionCancellationActivityOutput
	cancelSubscriptionInput := subscriptionModels.CheckSubscriptionCancellationActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		Period:         periodsOutput.Periods[len(periodsOutput.Periods)-1],
	}

	err = workflow.ExecuteActivity(ctx, ActivityCheckCancellation, cancelSubscriptionInput).Get(ctx, &cancelSubscriptionOutput)
	if err != nil {
		logger.Error("Failed to cancel subscription",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}

	// ================================================================================
	// STEP 7: Update Subscription Period
	// ================================================================================

	logger.Info("Step 7: Updating subscription period",
		"subscription_id", input.SubscriptionID)

	// Update to the new current period (last period from calculated periods)
	lastPeriod := periodsOutput.Periods[len(periodsOutput.Periods)-1]
	var updateSubscriptionPeriodOutput subscriptionModels.UpdateSubscriptionPeriodActivityOutput
	updateSubscriptionPeriodInput := subscriptionModels.UpdateSubscriptionPeriodActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		PeriodStart:    lastPeriod.Start,
		PeriodEnd:      lastPeriod.End,
	}

	err = workflow.ExecuteActivity(ctx, ActivityUpdateCurrentPeriod, updateSubscriptionPeriodInput).Get(ctx, &updateSubscriptionPeriodOutput)
	if err != nil {
		logger.Error("Failed to update subscription period",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}

	return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
		Success:     true,
		CompletedAt: workflow.Now(ctx),
	}, nil
}
