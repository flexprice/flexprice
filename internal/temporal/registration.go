package temporal

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/activities"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"github.com/flexprice/flexprice/internal/types"
)

// WorkerConfig defines the configuration for a specific task queue worker
type WorkerConfig struct {
	TaskQueue  types.TemporalTaskQueue
	Workflows  []interface{}
	Activities []interface{}
}

// RegisterWorkflowsAndActivities registers all workflows and activities with the temporal service
func RegisterWorkflowsAndActivities(temporalService temporalService.TemporalService, params service.ServiceParams) error {
	// Create activity instances with dependencies
	planService := service.NewPlanService(params)
	planActivities := activities.NewPlanActivities(planService)

	taskService := service.NewTaskService(params)
	taskActivities := activities.NewTaskActivities(taskService)

	// Get all task queues and register workflows/activities for each
	for _, taskQueue := range types.GetAllTaskQueues() {
		config := buildWorkerConfig(taskQueue, planActivities, taskActivities)
		if err := registerWorker(temporalService, config); err != nil {
			return fmt.Errorf("failed to register worker for task queue %s: %w", taskQueue, err)
		}
	}

	return nil
}

// buildWorkerConfig creates a worker configuration for a specific task queue
func buildWorkerConfig(taskQueue types.TemporalTaskQueue, planActivities *activities.PlanActivities, taskActivities *activities.TaskActivities) WorkerConfig {
	workflowsList := []interface{}{}
	activitiesList := []interface{}{}

	switch taskQueue {
	case types.TemporalTaskQueueTask:
		workflowsList = append(workflowsList, workflows.TaskProcessingWorkflow)
		activitiesList = append(activitiesList, taskActivities.ProcessTask)

	case types.TemporalTaskQueuePrice:
		workflowsList = append(workflowsList, workflows.PriceSyncWorkflow)
		activitiesList = append(activitiesList, planActivities.SyncPlanPrices)

		// Other task queues will be added when workflows are implemented
	}

	return WorkerConfig{
		TaskQueue:  taskQueue,
		Workflows:  workflowsList,
		Activities: activitiesList,
	}
}

// registerWorker registers workflows and activities for a specific task queue
func registerWorker(temporalService temporalService.TemporalService, config WorkerConfig) error {
	// Register workflows
	for i, workflow := range config.Workflows {
		if err := temporalService.RegisterWorkflow(config.TaskQueue, workflow); err != nil {
			return fmt.Errorf("failed to register workflow %d for task queue %s: %w", i, config.TaskQueue.String(), err)
		}
	}

	// Register activities
	for i, activity := range config.Activities {
		if err := temporalService.RegisterActivity(config.TaskQueue, activity); err != nil {
			return fmt.Errorf("failed to register activity %d for task queue %s: %w", i, config.TaskQueue.String(), err)
		}
	}

	return nil
}
