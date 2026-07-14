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
	WorkflowUsageAlert     = "UsageAlertWorkflow"
	ActivitySpendAlerts    = "SpendAlertsActivity"
	ActivityWalletAlerts   = "WalletAlertsActivity"

	usageAlertActivityBudget = 5 * time.Minute
)

// UsageAlertWorkflow is the debouncer target for usage-driven alert evaluation.
// StartDelay handles the debounce window server-side, so by the time this runs
// the customer is settled — no in-workflow sleep.
//
// Runs SpendAlertsActivity, then WalletAlertsActivity, sequentially. Each is
// self-contained (fetch + evaluate). A failure in one is logged but does not
// block the other.
func UsageAlertWorkflow(ctx workflow.Context, input models.UsageAlertWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}

	logger := workflow.GetLogger(ctx)
	logger.Debug("UsageAlertWorkflow firing",
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID,
		"customer_id", input.CustomerID,
	)

	activityInput := models.UsageAlertActivityInput{
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		CustomerID:    input.CustomerID,
	}

	actCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: usageAlertActivityBudget,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Second * 30,
			MaximumAttempts:    3,
		},
	})

	if err := workflow.ExecuteActivity(actCtx, ActivitySpendAlerts, activityInput).Get(actCtx, nil); err != nil {
		logger.Warn("SpendAlertsActivity returned error", "error", err)
	}
	if err := workflow.ExecuteActivity(actCtx, ActivityWalletAlerts, activityInput).Get(actCtx, nil); err != nil {
		logger.Warn("WalletAlertsActivity returned error", "error", err)
	}

	return nil
}
