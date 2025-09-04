package temporal

import (
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/activities"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"go.temporal.io/sdk/worker"
	"go.uber.org/zap"
)

// RegisterWorkflowsAndActivities registers all workflows and activities with a Temporal worker.
func RegisterWorkflowsAndActivities(w worker.Worker, params service.ServiceParams) {
	// Add debug logging
	logger := zap.NewExample()
	defer logger.Sync()

	// workflows - using function references (names will be the function names)
	logger.Info("Registering workflows")
	w.RegisterWorkflow(workflows.CronBillingWorkflow)      // "CronBillingWorkflow"
	w.RegisterWorkflow(workflows.CalculateChargesWorkflow) // "CalculateChargesWorkflow"
	w.RegisterWorkflow(workflows.PriceSyncWorkflow)        // "PriceSyncWorkflow"
	logger.Info("Workflows registered",
		zap.String("workflow1", "CronBillingWorkflow"),
		zap.String("workflow2", "CalculateChargesWorkflow"),
		zap.String("workflow3", "PriceSyncWorkflow"))

	// activities - properly instantiate with dependencies
	planService := service.NewPlanService(params)
	planActivities := activities.NewPlanActivities(planService)
	billingActivities := &activities.BillingActivities{}

	logger.Info("Registering activities")
	// Register activities with explicit method names to match workflow calls
	w.RegisterActivity(planActivities.SyncPlanPrices)       // "SyncPlanPrices"
	w.RegisterActivity(billingActivities.FetchDataActivity) // "FetchDataActivity"
	w.RegisterActivity(billingActivities.CalculateActivity) // "CalculateActivity"

	logger.Info("Activities registered",
		zap.String("activity1", "SyncPlanPrices"),
		zap.String("activity2", "FetchDataActivity"),
		zap.String("activity3", "CalculateActivity"))
}
