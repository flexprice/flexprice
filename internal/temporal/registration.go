package temporal

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/service"
	chargebeeActivities "github.com/flexprice/flexprice/internal/temporal/activities/chargebee"
	customerActivities "github.com/flexprice/flexprice/internal/temporal/activities/customer"
	eventsActivities "github.com/flexprice/flexprice/internal/temporal/activities/events"
	exportActivities "github.com/flexprice/flexprice/internal/temporal/activities/export"
	hubspotActivities "github.com/flexprice/flexprice/internal/temporal/activities/hubspot"
	invoiceActivities "github.com/flexprice/flexprice/internal/temporal/activities/invoice"
	moyasarActivities "github.com/flexprice/flexprice/internal/temporal/activities/moyasar"
	nomodActivities "github.com/flexprice/flexprice/internal/temporal/activities/nomod"
	paddleActivities "github.com/flexprice/flexprice/internal/temporal/activities/paddle"
	planActivities "github.com/flexprice/flexprice/internal/temporal/activities/plan"
	prepareProcessedEventsActivities "github.com/flexprice/flexprice/internal/temporal/activities/prepareprocessedevents"
	qbActivities "github.com/flexprice/flexprice/internal/temporal/activities/quickbooks"
	razorpayActivities "github.com/flexprice/flexprice/internal/temporal/activities/razorpay"
	stripeActivities "github.com/flexprice/flexprice/internal/temporal/activities/stripe"
	subscriptionActivities "github.com/flexprice/flexprice/internal/temporal/activities/subscription"
	taskActivities "github.com/flexprice/flexprice/internal/temporal/activities/task"
	workflowActivities "github.com/flexprice/flexprice/internal/temporal/activities/workflow"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	eventsWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/events"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	invoiceWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/invoice"
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
	// Create workflow tracking activity (follows standard activity pattern)
	workflowTrackingActivities := workflowActivities.NewWorkflowTrackingActivities(
		params,
		params.WorkflowExecutionRepo,
		params.Logger,
	)

	// Create activity instances with dependencies
	planService := service.NewPlanService(params)
	planActivities := planActivities.NewPlanActivities(planService)

	prepareEventsActivities := prepareProcessedEventsActivities.NewPrepareProcessedEventsActivities(params)

	taskService := service.NewTaskService(params)
	taskActivities := taskActivities.NewTaskActivities(taskService)

	// QuickBooks price sync activities
	qbPriceSyncActivities := qbActivities.NewQuickBooksPriceSyncActivities(
		params.IntegrationFactory,
		params.PlanRepo,
		params.PriceRepo,
		params.Logger,
	)

	// Export activities
	taskActivity := exportActivities.NewTaskActivity(params.TaskRepo, params.Logger)

	// Create scheduled task service for interval boundary calculations
	// Note: temporal client is nil because activity only uses CalculateIntervalBoundaries method
	scheduledTaskService := service.NewScheduledTaskService(
		params.ScheduledTaskRepo,
		params.ConnectionRepo,
		nil, // temporal client not needed for boundary calculations
		params.Logger,
		params.Config,
	)

	scheduledTaskActivity := exportActivities.NewScheduledTaskActivity(
		params.ScheduledTaskRepo,
		params.TaskRepo,
		params.Logger,
		scheduledTaskService,
	)
	// Create wallet service for credit usage export
	walletService := service.NewWalletService(params)
	exportActivity := exportActivities.NewExportActivity(params.FeatureUsageRepo, params.PriceRepo, params.InvoiceRepo, params.WalletRepo, walletService, params.CustomerRepo, params.ConnectionRepo, params.IntegrationFactory, params.Logger)

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

	invoiceActs := invoiceActivities.NewInvoiceActivities(
		params,
		params.Logger,
	)

	hubspotQuoteSyncActivities := hubspotActivities.NewQuoteSyncActivities(
		params.IntegrationFactory,
		params.Logger,
	)

	// Nomod activities - need to create customer service
	customerService := service.NewCustomerService(params)
	nomodInvoiceSyncActivities := nomodActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)

	// Moyasar activities
	moyasarInvoiceSyncActivities := moyasarActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)

	// Paddle activities
	paddleInvoiceSyncActivities := paddleActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)

	// Stripe/Razorpay/Chargebee/QuickBooks invoice sync activities
	stripeInvoiceSyncActivities := stripeActivities.NewInvoiceSyncActivities(
		params,
		params.Logger,
	)
	razorpayInvoiceSyncActivities := razorpayActivities.NewInvoiceSyncActivities(
		params,
		params.Logger,
	)
	chargebeeInvoiceSyncActivities := chargebeeActivities.NewInvoiceSyncActivities(
		params,
		params.Logger,
	)
	qbInvoiceSyncActivities := qbActivities.NewQuickBooksInvoiceSyncActivities(
		params,
		params.Logger,
	)

	// Customer activities
	customerActivities := customerActivities.NewCustomerActivities(
		params,
		params.Logger,
	)

	// Reprocess events activities
	featureUsageTrackingService := service.NewFeatureUsageTrackingService(
		params,
		params.EventRepo,
		params.FeatureUsageRepo,
	)
	reprocessEventsActivities := eventsActivities.NewReprocessEventsActivities(featureUsageTrackingService)

	// Reprocess raw events activities
	rawEventsReprocessingService := service.NewRawEventsReprocessingService(params)
	reprocessRawEventsActivities := eventsActivities.NewReprocessRawEventsActivities(rawEventsReprocessingService)

	// Get all task queues and register workflows/activities for each
	for _, taskQueue := range types.GetAllTaskQueues() {
		config := buildWorkerConfig(taskQueue, workflowTrackingActivities, planActivities, prepareEventsActivities, taskActivities, taskActivity, scheduledTaskActivity, exportActivity, hubspotDealSyncActivities, hubspotInvoiceSyncActivities, hubspotQuoteSyncActivities, qbPriceSyncActivities, nomodInvoiceSyncActivities, moyasarInvoiceSyncActivities, paddleInvoiceSyncActivities, stripeInvoiceSyncActivities, razorpayInvoiceSyncActivities, chargebeeInvoiceSyncActivities, qbInvoiceSyncActivities, customerActivities, scheduleBillingActivities, billingActivities, invoiceActs, reprocessEventsActivities, reprocessRawEventsActivities)
		if err := registerWorker(temporalService, config); err != nil {
			return fmt.Errorf("failed to register worker for task queue %s: %w", taskQueue, err)
		}
	}

	return nil
}

// buildWorkerConfig creates a worker configuration for a specific task queue
func buildWorkerConfig(
	taskQueue types.TemporalTaskQueue,
	workflowTrackingActivities *workflowActivities.WorkflowTrackingActivities,
	planActivities *planActivities.PlanActivities,
	prepareEventsActivities *prepareProcessedEventsActivities.PrepareProcessedEventsActivities,
	taskActivities *taskActivities.TaskActivities,
	taskActivity *exportActivities.TaskActivity,
	scheduledTaskActivity *exportActivities.ScheduledTaskActivity,
	exportActivity *exportActivities.ExportActivity,
	hubspotDealSyncActivities *hubspotActivities.DealSyncActivities,
	hubspotInvoiceSyncActivities *hubspotActivities.InvoiceSyncActivities,
	hubspotQuoteSyncActivities *hubspotActivities.QuoteSyncActivities,
	qbPriceSyncActivities *qbActivities.QuickBooksPriceSyncActivities,
	nomodInvoiceSyncActivities *nomodActivities.InvoiceSyncActivities,
	moyasarInvoiceSyncActivities *moyasarActivities.InvoiceSyncActivities,
	paddleInvoiceSyncActivities *paddleActivities.InvoiceSyncActivities,
	stripeInvoiceSyncActivities *stripeActivities.InvoiceSyncActivities,
	razorpayInvoiceSyncActivities *razorpayActivities.InvoiceSyncActivities,
	chargebeeInvoiceSyncActivities *chargebeeActivities.InvoiceSyncActivities,
	qbInvoiceSyncActivities *qbActivities.QuickBooksInvoiceSyncActivities,
	customerActivities *customerActivities.CustomerActivities,
	scheduleBillingActivities *subscriptionActivities.SubscriptionActivities,
	billingActivities *subscriptionActivities.BillingActivities,
	invoiceActs *invoiceActivities.InvoiceActivities,
	reprocessEventsActivities *eventsActivities.ReprocessEventsActivities,
	reprocessRawEventsActivities *eventsActivities.ReprocessRawEventsActivities,
) WorkerConfig {
	workflowsList := []interface{}{}
	// Add tracking activity to all task queues
	activitiesList := []interface{}{
		workflowTrackingActivities.TrackWorkflowStart,
		workflowTrackingActivities.TrackWorkflowEnd,
	}

	switch taskQueue {
	case types.TemporalTaskQueueTask:
		workflowsList = append(workflowsList,
			workflows.TaskProcessingWorkflow,
			workflows.HubSpotDealSyncWorkflow,
			workflows.HubSpotInvoiceSyncWorkflow,
			workflows.HubSpotQuoteSyncWorkflow,
			workflows.NomodInvoiceSyncWorkflow,
			workflows.MoyasarInvoiceSyncWorkflow,
			workflows.PaddleInvoiceSyncWorkflow,
			workflows.StripeInvoiceSyncWorkflow,
			workflows.RazorpayInvoiceSyncWorkflow,
			workflows.ChargebeeInvoiceSyncWorkflow,
			workflows.QuickBooksInvoiceSyncWorkflow,
			workflows.PrepareProcessedEventsWorkflow,
		)
		activitiesList = append(activitiesList,
			taskActivities.ProcessTask,
			hubspotDealSyncActivities.CreateLineItems,
			hubspotDealSyncActivities.UpdateDealAmount,
			hubspotInvoiceSyncActivities.SyncInvoiceToHubSpot,
			hubspotQuoteSyncActivities.CreateQuoteAndLineItems,
			nomodInvoiceSyncActivities.SyncInvoiceToNomod,
			moyasarInvoiceSyncActivities.SyncInvoiceToMoyasar,
			paddleInvoiceSyncActivities.SyncInvoiceToPaddle,
			stripeInvoiceSyncActivities.SyncInvoiceToStripe,
			razorpayInvoiceSyncActivities.SyncInvoiceToRazorpay,
			chargebeeInvoiceSyncActivities.SyncInvoiceToChargebee,
			qbInvoiceSyncActivities.SyncInvoiceToQuickBooks,
			prepareEventsActivities.CreateFeatureAndPriceActivity,
			prepareEventsActivities.RolloutToSubscriptionsActivity,
		)

	case types.TemporalTaskQueuePrice:
		workflowsList = append(workflowsList,
			workflows.PriceSyncWorkflow,
			workflows.QuickBooksPriceSyncWorkflow,
		)
		activitiesList = append(activitiesList,
			planActivities.SyncPlanPrices,
			qbPriceSyncActivities.SyncPriceToQuickBooks,
		)

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
			invoiceWorkflows.RecalculateInvoiceWorkflow,
		)
		activitiesList = append(activitiesList,
			// Schedule billing activities
			scheduleBillingActivities.ScheduleBillingActivity,
			// Subscription billing period activities
			billingActivities.CheckDraftSubscriptionActivity,
			billingActivities.CalculatePeriodsActivity,
			billingActivities.CreateDraftInvoicesActivity,
			billingActivities.UpdateCurrentPeriodActivity,
			billingActivities.CheckCancellationActivity,
			billingActivities.ProcessPendingPlanChangesActivity,
			billingActivities.TriggerInvoiceWorkflowActivity,
			// Invoice recalculation (v2)
			invoiceActs.RecalculateInvoiceActivity,
		)

	case types.TemporalTaskQueueInvoice:
		workflowsList = append(
			workflowsList,
			invoiceWorkflows.ProcessInvoiceWorkflow,
			invoiceWorkflows.FinalizeDraftInvoiceWorkflow,
			invoiceWorkflows.ScheduleDraftFinalizationWorkflow,
		)
		activitiesList = append(activitiesList,
			// Invoice workflow activities
			invoiceActs.ComputeInvoiceActivity,
			invoiceActs.FinalizeInvoiceActivity,
			invoiceActs.SyncInvoiceToVendorActivity,
			invoiceActs.AttemptInvoicePaymentActivity,
			// Draft finalization cron activity
			invoiceActs.FinalizeDueDraftsActivity,
		)

	case types.TemporalTaskQueueWorkflows:
		// Customer workflows
		workflowsList = append(workflowsList,
			workflows.CustomerOnboardingWorkflow,
			workflows.PrepareProcessedEventsWorkflow,
		)
		// Customer activities
		activitiesList = append(activitiesList,
			customerActivities.CreateCustomerActivity,
			customerActivities.CreateWalletActivity,
			customerActivities.CreateSubscriptionActivity,
			prepareEventsActivities.CreateFeatureAndPriceActivity,
			prepareEventsActivities.RolloutToSubscriptionsActivity,
			planActivities.SyncPlanPrices,
		)
	case types.TemporalTaskQueueReprocessEvents:
		workflowsList = append(workflowsList,
			eventsWorkflows.ReprocessEventsWorkflow,
			eventsWorkflows.ReprocessRawEventsWorkflow,
			eventsWorkflows.ReprocessEventsForPlanWorkflow,
		)
		activitiesList = append(activitiesList,
			reprocessEventsActivities.ReprocessEvents,
			reprocessRawEventsActivities.ReprocessRawEvents,
			planActivities.ReprocessEventsForPlan,
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
