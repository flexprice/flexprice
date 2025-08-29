package temporal

import (
	"github.com/flexprice/flexprice/internal/temporal/activities"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"go.temporal.io/sdk/worker"
)

// RegisterWorkflowsAndActivities registers all workflows and activities with a Temporal worker.
func RegisterWorkflowsAndActivities(w worker.Worker) {
	w.RegisterWorkflow(workflows.CronBillingWorkflow)
	w.RegisterWorkflow(workflows.CalculateChargesWorkflow)
	w.RegisterActivity(&activities.BillingActivities{})
	w.RegisterActivity(&activities.PlanActivities{})
}
