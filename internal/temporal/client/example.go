package client

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.uber.org/fx"
)

// InitializeTemporal initializes the temporal system
func InitializeTemporal(cfg *config.Configuration, logger *logger.Logger) (*temporal.TemporalClient, *WorkerManager, error) {
	// Get or create client
	client, err := GetTemporalClient(&cfg.Temporal, logger)
	if err != nil {
		return nil, nil, err
	}

	// Initialize worker manager
	workerManager := InitWorkerManager(client, logger)

	return client, workerManager, nil
}

// RegisterWorkflowsAndActivities registers workflows and activities with workers
func RegisterWorkflowsAndActivities(workerManager *WorkerManager) error {
	// Register workflows for different task queues using constants
	if err := workerManager.RegisterWorkflow(models.TaskProcessingTaskQueue, "TaskProcessingWorkflow"); err != nil {
		return err
	}

	if err := workerManager.RegisterWorkflow(models.PriceSyncTaskQueue, "PriceSyncWorkflow"); err != nil {
		return err
	}

	if err := workerManager.RegisterWorkflow(models.BillingTaskQueue, "CronBillingWorkflow"); err != nil {
		return err
	}

	return nil
}

// StartWorkers starts all registered workers
func StartWorkers(workerManager *WorkerManager) error {
	// Start workers for all task queues using constants
	taskQueues := models.GetAllTaskQueues()

	for _, taskQueue := range taskQueues {
		if err := workerManager.StartWorker(taskQueue); err != nil {
			return err
		}
	}

	return nil
}

// FxModule provides the temporal system as an fx module
var FxModule = fx.Module("temporal",
	fx.Provide(
		func(cfg *config.Configuration, logger *logger.Logger) (*temporal.TemporalClient, *WorkerManager, error) {
			return InitializeTemporal(cfg, logger)
		},
	),
	fx.Invoke(
		func(workerManager *WorkerManager) error {
			// Register workflows and activities
			if err := RegisterWorkflowsAndActivities(workerManager); err != nil {
				return err
			}

			// Start workers
			return StartWorkers(workerManager)
		},
	),
)
