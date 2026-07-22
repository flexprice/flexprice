package workflows

import (
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"

	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowUsageAlert = "UsageAlertWorkflow"

	// ActivitySpendAndEntitlementAlerts fuses subscription-spend evaluation with
	// per-grant refresh and exhaustion alerts; both halves share customer setup.
	ActivitySpendAndEntitlementAlerts = "SpendAndEntitlementAlertsActivity"

	ActivityWalletAlerts = "WalletAlertsActivity"

	usageAlertActivityBudget = 5 * time.Minute
)

// UsageAlertWorkflow is the debouncer target for usage-driven alert evaluation.
// Runs SpendAndEntitlementAlertsActivity then WalletAlertsActivity; the two are
// independent, so a spend-side failure is logged and wallet still runs.
//
// Staleness: input.ActivityStaleAfter maps to Temporal's ScheduleToStartTimeout —
// the out-of-the-box primitive for "this task waited too long in the queue".
// A schedule-to-start timeout is not retried by the retry policy (a retry would
// rejoin the same queue), so it surfaces here and we re-enqueue once after
// input.StaleRescheduleDelay. The fresh enqueue lands at the back of the queue,
// letting newer customers evaluate first while stale work still completes.
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

	opts := workflow.ActivityOptions{
		StartToCloseTimeout: usageAlertActivityBudget,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 2,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Second * 30,
			MaximumAttempts:    3,
		},
	}
	if input.ActivityStaleAfter > 0 {
		opts.ScheduleToStartTimeout = input.ActivityStaleAfter
	}
	actCtx := workflow.WithActivityOptions(ctx, opts)

	runActivity := func(name string) {
		err := workflow.ExecuteActivity(actCtx, name, activityInput).Get(actCtx, nil)
		if err != nil && isScheduleToStartTimeout(err) {
			logger.Info("activity waited too long in queue, re-enqueueing",
				"activity", name, "stale_after", input.ActivityStaleAfter.String())
			if input.StaleRescheduleDelay > 0 {
				if sleepErr := workflow.Sleep(ctx, input.StaleRescheduleDelay); sleepErr != nil {
					logger.Error("sleep before re-enqueue failed", "activity", name, "error", sleepErr)
					return
				}
			}
			err = workflow.ExecuteActivity(actCtx, name, activityInput).Get(actCtx, nil)
		}
		if err != nil {
			logger.Error("activity returned error", "activity", name, "error", err)
		}
	}

	runActivity(ActivitySpendAndEntitlementAlerts)
	runActivity(ActivityWalletAlerts)

	return nil
}

func isScheduleToStartTimeout(err error) bool {
	var timeoutErr *temporal.TimeoutError
	if errors.As(err, &timeoutErr) {
		return timeoutErr.TimeoutType() == enumspb.TIMEOUT_TYPE_SCHEDULE_TO_START
	}
	return false
}
