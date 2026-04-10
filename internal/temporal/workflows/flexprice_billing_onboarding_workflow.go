package workflows

import (
	"time"

	flexpricebillingactivities "github.com/flexprice/flexprice/internal/temporal/activities/flexprice_billing"
	flexpricebillingmodels "github.com/flexprice/flexprice/internal/temporal/models/flexprice_billing"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

// FlexpriceBillingOnboardingWorkflow is a Temporal workflow that onboards a new Flexprice tenant
// into the self-billing system. It creates a customer in the billing tenant and assigns the
// configured BASE plan subscription.
//
// The workflow is triggered asynchronously from CreateTenant so the API call is non-blocking.
func FlexpriceBillingOnboardingWorkflow(
	ctx workflow.Context,
	input flexpricebillingmodels.FlexpriceBillingOnboardingInput,
) (*flexpricebillingmodels.FlexpriceBillingOnboardingResult, error) {
	logger := workflow.GetLogger(ctx)
	logger.Info("Starting FlexpriceBillingOnboardingWorkflow", "tenant_id", input.TenantID)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    5 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	result := &flexpricebillingmodels.FlexpriceBillingOnboardingResult{Status: "processing"}

	// Step 1: Create customer in billing tenant
	var customerID string
	if err := workflow.ExecuteActivity(ctx,
		(*flexpricebillingactivities.FlexpriceBillingActivities).CreateBillingCustomerActivity,
		input,
	).Get(ctx, &customerID); err != nil {
		errMsg := err.Error()
		result.Status = "failed"
		result.ErrorSummary = errMsg
		logger.Error("CreateBillingCustomerActivity failed", "tenant_id", input.TenantID, "error", errMsg)
		return result, nil
	}
	result.CustomerID = customerID

	// Step 2: Assign BASE plan subscription
	var subscriptionID string
	if err := workflow.ExecuteActivity(ctx,
		(*flexpricebillingactivities.FlexpriceBillingActivities).CreateBillingSubscriptionActivity,
		input,
		customerID,
	).Get(ctx, &subscriptionID); err != nil {
		errMsg := err.Error()
		result.Status = "failed"
		result.ErrorSummary = errMsg
		logger.Error("CreateBillingSubscriptionActivity failed", "tenant_id", input.TenantID, "error", errMsg)
		return result, nil
	}
	result.SubscriptionID = subscriptionID
	result.Status = "completed"

	logger.Info("FlexpriceBillingOnboardingWorkflow completed",
		"tenant_id", input.TenantID,
		"customer_id", customerID,
		"subscription_id", subscriptionID)

	return result, nil
}
