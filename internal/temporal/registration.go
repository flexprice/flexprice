package temporal

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/activities"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"github.com/flexprice/flexprice/internal/types"
)

// WorkerConfig defines the configuration for a specific task queue worker
type WorkerConfig struct {
	TaskQueue  string
	Workflows  []interface{}
	Activities []interface{}
}

// RegisterWorkflowsAndActivities registers all workflows and activities with the temporal service
func RegisterWorkflowsAndActivities(temporalService interface{}, params service.ServiceParams) error {
	// Create activity instances with dependencies
	planService := service.NewPlanService(params)
	planActivities := activities.NewPlanActivities(planService)

	taskService := service.NewTaskService(params)
	taskActivities := activities.NewTaskActivities(taskService)

	// Define worker configurations for each task queue
	workerConfigs := []WorkerConfig{
		{
			TaskQueue: types.TemporalTaskProcessingWorkflow.TaskQueueName(),
			Workflows: []interface{}{
				workflows.TaskProcessingWorkflow,
			},
			Activities: []interface{}{
				taskActivities.ProcessTask,
			},
		},
		{
			TaskQueue: types.TemporalPriceSyncWorkflow.TaskQueueName(),
			Workflows: []interface{}{
				workflows.PriceSyncWorkflow,
			},
			Activities: []interface{}{
				planActivities.SyncPlanPrices,
			},
		},
		{
			TaskQueue: types.TemporalBillingWorkflow.TaskQueueName(),
			Workflows: []interface{}{
				// Add billing workflows here when available
			},
			Activities: []interface{}{
				// Add billing activities here when available
			},
		},
	}

	// Register workflows and activities for each worker configuration
	for _, config := range workerConfigs {
		if err := registerWorker(temporalService, config); err != nil {
			return fmt.Errorf("failed to register worker for task queue %s: %w", config.TaskQueue, err)
		}
	}

	return nil
}

// registerWorker registers workflows and activities for a specific task queue
func registerWorker(temporalService interface{}, config WorkerConfig) error {
	// Type assertion to get the temporal service
	service, ok := temporalService.(interface {
		RegisterWorkflow(taskQueue string, workflow interface{}) error
		RegisterActivity(taskQueue string, activity interface{}) error
	})
	if !ok {
		return fmt.Errorf("temporal service does not implement required registration methods")
	}

	// Register workflows
	for i, workflow := range config.Workflows {
		if err := service.RegisterWorkflow(config.TaskQueue, workflow); err != nil {
			return fmt.Errorf("failed to register workflow %d for task queue %s: %w", i, config.TaskQueue, err)
		}
	}

	// Register activities
	for i, activity := range config.Activities {
		if err := service.RegisterActivity(config.TaskQueue, activity); err != nil {
			return fmt.Errorf("failed to register activity %d for task queue %s: %w", i, config.TaskQueue, err)
		}
	}

	return nil
}
