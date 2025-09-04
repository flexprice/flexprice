package temporal

import (
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/activities"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"go.temporal.io/sdk/worker"
)

// RegisterWorkflowsAndActivities registers all workflows and activities with a Temporal worker.
func RegisterWorkflowsAndActivities(w worker.Worker, params service.ServiceParams) {

	// workflows
	w.RegisterWorkflow(workflows.CronBillingWorkflow)
	w.RegisterWorkflow(workflows.CalculateChargesWorkflow)
	w.RegisterWorkflow(workflows.PriceSyncWorkflow)

	// activities - properly instantiate with dependencies
	planService := service.NewPlanService(params)
	planActivities := activities.NewPlanActivities(planService)

	billingActivities := &activities.BillingActivities{}

	w.RegisterActivity(planActivities)
	w.RegisterActivity(billingActivities)
}
