package subscription

import (
	"time"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
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
	ActivityCreateDraftInvoices    = "CreateDraftInvoicesActivity"
	ActivityUpdateCurrentPeriod    = "UpdateCurrentPeriodActivity"
	ActivityCheckCancellation      = "CheckCancellationActivity"
	ActivityProcessPlanChange      = "ProcessPendingPlanChangesActivity"
	// Activity from invoice package
	ActivityTriggerInvoiceWorkflow = "TriggerInvoiceWorkflowActivity"
)

// ProcessSubscriptionBillingWorkflow processes a subscription billing workflow
// This workflow orchestrates the subscription billing period processing:
// 1. Check if subscription is draft
// 2. Calculate billing periods up to current time
// 3. For each period (except the last), create draft invoice
// 4. Check for cancellation
// 5. Process pending plan changes at period end
// 6. Update subscription to new current period
// 7. Trigger invoice workflows for processing (fire-and-forget)
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
		UserID:         input.UserID,
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
		UserID:         input.UserID,
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
	// STEP 3: Create Draft Invoices (without finalization)
	// ================================================================================
	logger.Info("Step 3: Creating draft invoices",
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
		UserID:         input.UserID,
		Periods:        completedPeriods,
	}

	err = workflow.ExecuteActivity(ctx, ActivityCreateDraftInvoices, createInvoicesInput).Get(ctx, &createInvoicesOutput)
	if err != nil {
		logger.Error("Failed to create draft invoices",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}

	logger.Info("Created draft invoices",
		"subscription_id", input.SubscriptionID,
		"invoice_count", len(createInvoicesOutput.InvoiceIDs))

	// ================================================================================
	// STEP 4: Update Subscription Period
	// ================================================================================

	logger.Info("Step 4: Updating subscription period",
		"subscription_id", input.SubscriptionID)

	// Update to the new current period (last period from calculated periods)
	lastPeriod := periodsOutput.Periods[len(periodsOutput.Periods)-1]
	var updateSubscriptionPeriodOutput subscriptionModels.UpdateSubscriptionPeriodActivityOutput
	updateSubscriptionPeriodInput := subscriptionModels.UpdateSubscriptionPeriodActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		UserID:         input.UserID,
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

	// ================================================================================
	// STEP 5: Cancellation Check
	// ================================================================================
	logger.Info("Step 5: Checking for subscription cancellation",
		"subscription_id", input.SubscriptionID)
	var cancelSubscriptionOutput subscriptionModels.CheckSubscriptionCancellationActivityOutput
	cancelSubscriptionInput := subscriptionModels.CheckSubscriptionCancellationActivityInput{
		SubscriptionID: input.SubscriptionID,
		TenantID:       input.TenantID,
		EnvironmentID:  input.EnvironmentID,
		UserID:         input.UserID,
		Period:         lastPeriod,
	}

	err = workflow.ExecuteActivity(ctx, ActivityCheckCancellation, cancelSubscriptionInput).Get(ctx, &cancelSubscriptionOutput)
	if err != nil {
		logger.Error("Failed to check subscription cancellation",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return nil, err
	}

	// ================================================================================
	// STEP 6: Process Pending Plan Changes (only if subscription is still active)
	// ================================================================================
	if !cancelSubscriptionOutput.IsCancelled {
		logger.Info("Step 6: Processing pending plan changes",
			"subscription_id", input.SubscriptionID)

		var planChangeOutput subscriptionModels.ProcessPendingPlanChangesActivityOutput
		planChangeInput := subscriptionModels.ProcessPendingPlanChangesActivityInput{
			SubscriptionID: input.SubscriptionID,
			TenantID:       input.TenantID,
			EnvironmentID:  input.EnvironmentID,
			UserID:         input.UserID,
		}

		err = workflow.ExecuteActivity(ctx, ActivityProcessPlanChange, planChangeInput).Get(ctx, &planChangeOutput)
		if err != nil {
			// Log error but don't fail the workflow - plan changes can be retried
			logger.Warn("Failed to process pending plan changes, but continuing",
				"error", err,
				"subscription_id", input.SubscriptionID)
		} else if planChangeOutput.Success {
			logger.Info("Processed pending plan changes",
				"subscription_id", input.SubscriptionID,
				"was_changed", planChangeOutput.WasChanged)
		}
	} else {
		logger.Info("Step 6: Skipping plan change processing (subscription is cancelled)",
			"subscription_id", input.SubscriptionID)
	}

	// ================================================================================
	// STEP 7: Trigger Invoice Workflows (fire-and-forget)
	// ================================================================================

	// Only trigger if there are invoices to process
	if len(createInvoicesOutput.InvoiceIDs) > 0 {
		logger.Info("Step 7: Triggering invoice workflows",
			"subscription_id", input.SubscriptionID,
			"invoice_count", len(createInvoicesOutput.InvoiceIDs))

		var triggerOutput invoiceModels.TriggerInvoiceWorkflowActivityOutput
		triggerInput := invoiceModels.TriggerInvoiceWorkflowActivityInput{
			InvoiceIDs:    createInvoicesOutput.InvoiceIDs,
			TenantID:      input.TenantID,
			EnvironmentID: input.EnvironmentID,
			UserID:        input.UserID,
		}

		err = workflow.ExecuteActivity(ctx, ActivityTriggerInvoiceWorkflow, triggerInput).Get(ctx, &triggerOutput)
		if err != nil {
			// Log error but don't fail the workflow - invoice workflows can be triggered manually
			logger.Warn("Failed to trigger invoice workflows, but continuing",
				"error", err,
				"subscription_id", input.SubscriptionID)
		} else {
			logger.Info("Triggered invoice workflows",
				"subscription_id", input.SubscriptionID,
				"triggered_count", triggerOutput.TriggeredCount,
				"failed_count", triggerOutput.FailedCount)
		}
	} else {
		logger.Info("Step 7: No invoices to process, skipping invoice workflow triggers",
			"subscription_id", input.SubscriptionID)
	}

	return &subscriptionModels.ProcessSubscriptionBillingWorkflowResult{
		Success:     true,
		CompletedAt: workflow.Now(ctx),
	}, nil
}
