package workflows

import (
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"

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
// Staleness (two distinct queues, two mechanisms):
//
//   - Workflow run fired late: under a workflow-task backlog, a run scheduled
//     for 5:07 may only reach a worker at 6:10. Instead of doing hour-old work
//     ahead of fresher runs, it re-schedules ONCE via ContinueAsNew — the same
//     workflow ID atomically gets a fresh run whose first task lands at the
//     BACK of the queue, so already-queued newer customers evaluate first.
//     No sleeping, no duplicate-workflow race (same ID chain).
//     AlreadyRescheduled caps it to one hand-off per chain so a sustained
//     backlog can't livelock.
//
//   - Activity waited too long in the activity queue: bounded by
//     ScheduleToStartTimeout (= StaleAfter). Not retried by the retry policy
//     (a retry would rejoin the same queue); the error is logged and the next
//     event's workflow re-evaluates the customer.
func UsageAlertWorkflow(ctx workflow.Context, input models.UsageAlertWorkflowInput) error {
	if err := input.Validate(); err != nil {
		return err
	}

	logger := workflow.GetLogger(ctx)

	if !input.AlreadyRescheduled && input.StaleAfter > 0 && !input.ScheduledFor.IsZero() {
		age := workflow.Now(ctx).Sub(input.ScheduledFor)
		if age > input.StaleAfter {
			logger.Info("workflow run is stale, re-scheduling a fresh run at the back of the queue",
				"customer_id", input.CustomerID,
				"scheduled_for", input.ScheduledFor,
				"age", age.String(),
			)

			next := input
			next.AlreadyRescheduled = true
			next.ScheduledFor = workflow.Now(ctx)
			return workflow.NewContinueAsNewError(ctx, UsageAlertWorkflow, next)
		}
	}

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
	if input.StaleAfter > 0 {
		opts.ScheduleToStartTimeout = input.StaleAfter
	}
	actCtx := workflow.WithActivityOptions(ctx, opts)

	if err := workflow.ExecuteActivity(actCtx, ActivitySpendAndEntitlementAlerts, activityInput).Get(actCtx, nil); err != nil {
		logger.Error("SpendAndEntitlementAlertsActivity returned error", "error", err)
	}
	if err := workflow.ExecuteActivity(actCtx, ActivityWalletAlerts, activityInput).Get(actCtx, nil); err != nil {
		logger.Error("WalletAlertsActivity returned error", "error", err)
	}

	return nil
}
