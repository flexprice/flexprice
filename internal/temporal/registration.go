package temporal

import (
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/activities"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"go.temporal.io/sdk/worker"
)

// RegisterWorkflowsAndActivities registers all workflows and activities with a Temporal worker.
func RegisterWorkflowsAndActivities(w worker.Worker, params service.ServiceParams) {

	// activities - properly instantiate with dependencies
	planService := service.NewPlanService(params)
	planActivities := activities.NewPlanActivities(planService)
	billingActivities := &activities.BillingActivities{}

	// Create task activities
	taskService := service.NewTaskService(params)
	taskActivities := activities.NewTaskActivities(taskService)

	// workflows - using function references
	w.RegisterWorkflow(workflows.CronBillingWorkflow)      // "CronBillingWorkflow"
	w.RegisterWorkflow(workflows.CalculateChargesWorkflow) // "CalculateChargesWorkflow"
	w.RegisterWorkflow(workflows.PriceSyncWorkflow)        // "PriceSyncWorkflow"
	w.RegisterWorkflow(workflows.TaskProcessingWorkflow)   // "TaskProcessingWorkflow"

	// Register activities with explicit method names
	w.RegisterActivity(planActivities.SyncPlanPrices)       // "SyncPlanPrices"
	w.RegisterActivity(billingActivities.FetchDataActivity) // "FetchDataActivity"
	w.RegisterActivity(billingActivities.CalculateActivity) // "CalculateActivity"
	w.RegisterActivity(taskActivities.ProcessTask)          // "ProcessTask"

}
