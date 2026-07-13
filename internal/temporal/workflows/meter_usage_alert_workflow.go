package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	// Workflow and activity names — must match the registered function names in
	// registration.go so the SDK can dispatch by string.
	WorkflowMeterUsageAlert       = "MeterUsageAlertWorkflow"
	ActivityCheckSpendBreach      = "CheckSpendBreachActivity"
	ActivityCheckWalletBalance    = "CheckWalletBalanceActivity"
	meterUsageAlertActivityBudget = 5 * time.Minute
)

// MeterUsageAlertWorkflow is the debouncer target for meter-usage-driven alerts.
// The workflow is armed with StartDelay by the caller (meter usage post-insert),
// so by the time it starts executing here the debounce window has already elapsed
// server-side — no in-workflow sleep is required. Both alert paths run sequentially;
// each activity swallows its own errors so a failure in one does not skip the other.
func MeterUsageAlertWorkflow(ctx workflow.Context, input models.MeterUsageAlertWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}

	logger := workflow.GetLogger(ctx)
	logger.Debug("MeterUsageAlertWorkflow firing",
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID,
		"customer_id", input.CustomerID,
	)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: meterUsageAlertActivityBudget,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Second * 30,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	activityInput := models.MeterUsageAlertActivityInput{
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		CustomerID:    input.CustomerID,
	}

	// Spend-breach evaluation and wallet-balance check are independent — one
	// failing shouldn't block the other. Errors are logged inside the activity;
	// the workflow only propagates infrastructure-level failures (e.g. the
	// activity worker being unavailable) via Get.
	if err := workflow.ExecuteActivity(ctx, ActivityCheckSpendBreach, activityInput).Get(ctx, nil); err != nil {
		logger.Warn("CheckSpendBreachActivity returned error, continuing to wallet check", "error", err)
	}
	if err := workflow.ExecuteActivity(ctx, ActivityCheckWalletBalance, activityInput).Get(ctx, nil); err != nil {
		logger.Warn("CheckWalletBalanceActivity returned error", "error", err)
	}

	return nil
}
