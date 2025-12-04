package temporal

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/service"
	exportActivities "github.com/flexprice/flexprice/internal/temporal/activities/export"
	hubspotActivities "github.com/flexprice/flexprice/internal/temporal/activities/hubspot"
	planActivities "github.com/flexprice/flexprice/internal/temporal/activities/plan"
	subscriptionActivities "github.com/flexprice/flexprice/internal/temporal/activities/subscription"
	taskActivities "github.com/flexprice/flexprice/internal/temporal/activities/task"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	subscriptionWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/subscription"
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
	planActivities := planActivities.NewPlanActivities(planService)

	taskService := service.NewTaskService(params)
	taskActivities := taskActivities.NewTaskActivities(taskService)

	// Export activities
	taskActivity := exportActivities.NewTaskActivity(params.TaskRepo, params.Logger)

	// Create scheduled task service for interval boundary calculations
	// Note: temporal client is nil because activity only uses CalculateIntervalBoundaries method
	scheduledTaskService := service.NewScheduledTaskService(
		params.ScheduledTaskRepo,
		nil, // temporal client not needed for boundary calculations
		params.Logger,
	)

	scheduledTaskActivity := exportActivities.NewScheduledTaskActivity(
		params.ScheduledTaskRepo,
		params.TaskRepo,
		params.Logger,
		scheduledTaskService,
	)
	exportActivity := exportActivities.NewExportActivity(params.FeatureUsageRepo, params.InvoiceRepo, params.WalletRepo, params.ConnectionRepo, params.IntegrationFactory, params.Logger)

	// HubSpot activities - clean and simple, delegates to existing services
	hubspotDealSyncActivities := hubspotActivities.NewDealSyncActivities(
		params.IntegrationFactory,
		params.Logger,
	)

	hubspotInvoiceSyncActivities := hubspotActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		params.Logger,
	)

	subscriptionService := service.NewSubscriptionService(params)

	scheduleBillingActivities := subscriptionActivities.NewSubscriptionActivities(subscriptionService)
	billingActivities := subscriptionActivities.NewBillingActivities(
		subscriptionService,
		params,
		params.Logger,
	)
	// Get all task queues and register workflows/activities for each
	for _, taskQueue := range types.GetAllTaskQueues() {
		config := buildWorkerConfig(taskQueue, planActivities, taskActivities, taskActivity, scheduledTaskActivity, exportActivity, hubspotDealSyncActivities, hubspotInvoiceSyncActivities, subscriptionService, scheduleBillingActivities, billingActivities)
		if err := registerWorker(temporalService, config); err != nil {
			return fmt.Errorf("failed to register worker for task queue %s: %w", taskQueue, err)
		}
	}

	return nil
}

// buildWorkerConfig creates a worker configuration for a specific task queue
func buildWorkerConfig(
	taskQueue types.TemporalTaskQueue,
	planActivities *planActivities.PlanActivities,
	taskActivities *taskActivities.TaskActivities,
	taskActivity *exportActivities.TaskActivity,
	scheduledTaskActivity *exportActivities.ScheduledTaskActivity,
	exportActivity *exportActivities.ExportActivity,
	hubspotDealSyncActivities *hubspotActivities.DealSyncActivities,
	hubspotInvoiceSyncActivities *hubspotActivities.InvoiceSyncActivities,
	subscriptionService service.SubscriptionService,
	scheduleBillingActivities *subscriptionActivities.SubscriptionActivities,
	billingActivities *subscriptionActivities.BillingActivities,
) WorkerConfig {
	workflowsList := []interface{}{}
	activitiesList := []interface{}{}

	switch taskQueue {
	case types.TemporalTaskQueueTask:
		workflowsList = append(workflowsList,
			workflows.TaskProcessingWorkflow,
			workflows.HubSpotDealSyncWorkflow,
			workflows.HubSpotInvoiceSyncWorkflow,
		)
		activitiesList = append(activitiesList,
			taskActivities.ProcessTask,
			hubspotDealSyncActivities.CreateLineItems,
			hubspotDealSyncActivities.UpdateDealAmount,
			hubspotInvoiceSyncActivities.SyncInvoiceToHubSpot,
		)

	case types.TemporalTaskQueuePrice:
		workflowsList = append(workflowsList, workflows.PriceSyncWorkflow)
		activitiesList = append(activitiesList, planActivities.SyncPlanPrices)

	case types.TemporalTaskQueueExport:
		// Export workflows
		workflowsList = append(workflowsList,
			exportWorkflows.ExecuteExportWorkflow,
		)
		// Export activities
		activitiesList = append(activitiesList,
			taskActivity.CreateTask,
			taskActivity.UpdateTaskStatus,
			taskActivity.CompleteTask,
			scheduledTaskActivity.GetScheduledTaskDetails,
			exportActivity.ExportData,
		)
	case types.TemporalTaskQueueSubscription:
		workflowsList = append(
			workflowsList,
			subscriptionWorkflows.ScheduleSubscriptionBillingWorkflow,
			subscriptionWorkflows.ProcessSubscriptionBillingWorkflow,
		)
		activitiesList = append(activitiesList,
			scheduleBillingActivities.ScheduleBillingActivity,
			billingActivities.CheckPauseActivity,
			billingActivities.CalculatePeriodsActivity,
			billingActivities.ProcessPeriodsActivity,
			billingActivities.CreateInvoicesActivity,
			billingActivities.UpdateCurrentPeriodActivity,
			billingActivities.SyncInvoiceActivity,
			billingActivities.AttemptPaymentActivity,
			billingActivities.CheckCancellationActivity,
		)
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
