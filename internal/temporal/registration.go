package temporal

import (
	"fmt"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/integration/awsmarketplace"
	alertActivities "github.com/flexprice/flexprice/internal/temporal/activities/alerts"
	chargebeeActivities "github.com/flexprice/flexprice/internal/temporal/activities/chargebee"
	cronActivities "github.com/flexprice/flexprice/internal/temporal/activities/cron"
	customerActivities "github.com/flexprice/flexprice/internal/temporal/activities/customer"
	environmentActivities "github.com/flexprice/flexprice/internal/temporal/activities/environment"
	eventsActivities "github.com/flexprice/flexprice/internal/temporal/activities/events"
	exportActivities "github.com/flexprice/flexprice/internal/temporal/activities/export"
	hubspotActivities "github.com/flexprice/flexprice/internal/temporal/activities/hubspot"
	invoiceActivities "github.com/flexprice/flexprice/internal/temporal/activities/invoice"
	marketplaceActivities "github.com/flexprice/flexprice/internal/temporal/activities/marketplace"
	moyasarActivities "github.com/flexprice/flexprice/internal/temporal/activities/moyasar"
	nomodActivities "github.com/flexprice/flexprice/internal/temporal/activities/nomod"
	paddleActivities "github.com/flexprice/flexprice/internal/temporal/activities/paddle"
	planActivities "github.com/flexprice/flexprice/internal/temporal/activities/plan"
	prepareProcessedEventsActivities "github.com/flexprice/flexprice/internal/temporal/activities/prepareprocessedevents"
	qbActivities "github.com/flexprice/flexprice/internal/temporal/activities/quickbooks"
	razorpayActivities "github.com/flexprice/flexprice/internal/temporal/activities/razorpay"
	stripeActivities "github.com/flexprice/flexprice/internal/temporal/activities/stripe"
	subscriptionActivities "github.com/flexprice/flexprice/internal/temporal/activities/subscription"
	tabsActivities "github.com/flexprice/flexprice/internal/temporal/activities/tabs"
	taskActivities "github.com/flexprice/flexprice/internal/temporal/activities/task"
	whopActivities "github.com/flexprice/flexprice/internal/temporal/activities/whop"
	workflowActivities "github.com/flexprice/flexprice/internal/temporal/activities/workflow"
	zohoActivities "github.com/flexprice/flexprice/internal/temporal/activities/zoho"
	temporalService "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	cronWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/cron"
	eventsWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/events"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	invoiceWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/invoice"
	subscriptionWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/webhook"
)

// WorkerConfig defines the configuration for a specific task queue worker
type WorkerConfig struct {
	TaskQueue  types.TemporalTaskQueue
	Workflows  []interface{}
	Activities []interface{}
}

// cronActivityBundle groups activities registered on the Temporal "cron" task queue only.
type cronActivityBundle struct {
	creditGrant                  *cronActivities.CreditGrantActivities
	subscription                 *cronActivities.SubscriptionCronActivities
	walletCreditExpiry           *cronActivities.WalletCreditExpiryActivities
	webhookOutboundRetry         *cronActivities.WebhookOutboundRetryActivities
	paddleInvoicePullSync        *cronActivities.PaddleInvoicePullSyncActivities
	moyasarAuthPaymentSettlement *cronActivities.MoyasarAuthPaymentSettlementActivities
	checkoutSessionExpiry        *cronActivities.CheckoutSessionExpiryActivities
	marketplaceSnapshot          *marketplaceActivities.SnapshotActivities
	marketplaceReport            *marketplaceActivities.ReportActivities
}

// RegisterWorkflowsAndActivities registers all workflows and activities with the temporal service
func RegisterWorkflowsAndActivities(
	temporalService temporalService.TemporalService,
	params service.ServiceParams,
	webhookService *webhook.WebhookService,
) error {
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

	// Feature usage tracking (export needs GetDetailedUsageAnalytics for usage_analytics entity type)
	featureUsageTrackingService := service.NewFeatureUsageTrackingService(
		params,
		params.EventRepo,
		params.FeatureUsageRepo,
	)

	// Create wallet service for credit usage export
	walletService := service.NewWalletService(params)
	exportActivity := exportActivities.NewExportActivity(
		params.FeatureUsageRepo,
		params.MeterUsageRepo,
		params.PriceRepo,
		params.InvoiceRepo,
		params.WalletRepo,
		walletService,
		params.CustomerRepo,
		params.ConnectionRepo,
		params.IntegrationFactory,
		params.Config,
		params.Logger,
		featureUsageTrackingService,
		params.EventRepo,
		params.SubscriptionLineItemRepo,
	)

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
	nomodCustomerSyncActivities := nomodActivities.NewCustomerSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)

	// Whop activities
	whopInvoiceSyncActivities := whopActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)

	// Moyasar activities
	moyasarInvoiceService := service.NewInvoiceService(params)
	moyasarInvoiceSyncActivities := moyasarActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		customerService,
		moyasarInvoiceService,
		params.Logger,
	)

	// Paddle activities
	paddleInvoiceSyncActivities := paddleActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)
	paddleCustomerSyncActivities := paddleActivities.NewCustomerSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.InvoiceRepo,
		params.Logger,
	)
	paddleSubscriptionSyncActivities := paddleActivities.NewSubscriptionSyncActivities(
		params.IntegrationFactory,
		params.Logger,
	)

	// Stripe/Razorpay/Chargebee/QuickBooks invoice sync activities
	stripeInvoiceSyncActivities := stripeActivities.NewInvoiceSyncActivities(
		params,
		params.Logger,
	)
	stripeCustomerSyncActivities := stripeActivities.NewCustomerSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)
	razorpayInvoiceSyncActivities := razorpayActivities.NewInvoiceSyncActivities(
		params,
		params.Logger,
	)
	razorpayCustomerSyncActivities := razorpayActivities.NewCustomerSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)
	chargebeeInvoiceSyncActivities := chargebeeActivities.NewInvoiceSyncActivities(
		params,
		params.Logger,
	)
	chargebeeCustomerSyncActivities := chargebeeActivities.NewCustomerSyncActivities(
		params.IntegrationFactory,
		params.Logger,
	)
	qbInvoiceSyncActivities := qbActivities.NewQuickBooksInvoiceSyncActivities(
		params,
		params.Logger,
	)
	qbCustomerSyncActivities := qbActivities.NewQuickBooksCustomerSyncActivities(
		params.IntegrationFactory,
		customerService,
		params.Logger,
	)
	zohoInvoiceSyncActivities := zohoActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		params.Logger,
	)
	tabsInvoiceSyncActivities := tabsActivities.NewInvoiceSyncActivities(
		params.IntegrationFactory,
		params.Logger,
	)

	// Customer activities
	customerActivities := customerActivities.NewCustomerActivities(
		params,
		params.Logger,
	)

	// Meter-usage-driven alert activities (spend-breach + wallet-balance checks).
	// Registered on TemporalTaskQueueWorkflows alongside CustomerOnboardingWorkflow;
	// both are event-driven, low-frequency-per-customer, short-lived workflows.
	alertActs := alertActivities.NewAlertActivities(params, params.Logger)

	// Environment clone activities
	envActivities := environmentActivities.NewEnvironmentActivities(params)

	// Reprocess events activities
	reprocessEventsActivities := eventsActivities.NewReprocessEventsActivities(featureUsageTrackingService)

	// Reprocess raw events activities
	rawEventsReprocessingService := service.NewRawEventsReprocessingService(params)
	reprocessRawEventsActivities := eventsActivities.NewReprocessRawEventsActivities(rawEventsReprocessingService)

	// Cron workflow activities (reuses subscriptionService and walletService from above)
	creditGrantService := service.NewCreditGrantService(params)
	tenantService := service.NewTenantService(params)
	envAccessService := service.NewEnvAccessService(params.Config)
	settingsService := service.NewSettingsService(params)
	environmentService := service.NewEnvironmentService(params.EnvironmentRepo, envAccessService, settingsService, params)
	// Marketplace activities
	billingService := service.NewBillingService(params)
	awsMarketplaceClient := awsmarketplace.NewClient(params.Logger)
	marketplaceSnapshotActivities := marketplaceActivities.NewSnapshotActivities(
		subscriptionService,
		billingService,
		params.ConnectionRepo,
		params.EntityIntegrationMappingRepo,
		params.SubRepo,
		params.CustomerRepo,
		params.UsageRecordRepo,
		params.Logger,
	)
	marketplaceReportActivities := marketplaceActivities.NewReportActivities(
		params.ConnectionRepo,
		params.EntityIntegrationMappingRepo,
		params.UsageRecordRepo,
		params.EncryptionService,
		awsMarketplaceClient,
		params.Logger,
	)

	cronBundle := &cronActivityBundle{
		creditGrant:                  cronActivities.NewCreditGrantActivities(creditGrantService),
		subscription:                 cronActivities.NewSubscriptionCronActivities(subscriptionService, params.Logger),
		walletCreditExpiry:           cronActivities.NewWalletCreditExpiryActivities(walletService, tenantService, environmentService, params.Logger),
		webhookOutboundRetry:         cronActivities.NewWebhookOutboundRetryActivities(webhookService, params.Logger),
		paddleInvoicePullSync:        cronActivities.NewPaddleInvoicePullSyncActivities(params.InvoiceRepo, temporalService, params.Logger),
		moyasarAuthPaymentSettlement: cronActivities.NewMoyasarAuthPaymentSettlementActivities(params.IntegrationFactory, params.PaymentRepo, params.Logger),
		checkoutSessionExpiry:        cronActivities.NewCheckoutSessionExpiryActivities(service.NewCheckoutSessionService(params), params.Logger),
		marketplaceSnapshot:          marketplaceSnapshotActivities,
		marketplaceReport:            marketplaceReportActivities,
	}

	// Get all task queues and register workflows/activities for each
	for _, taskQueue := range types.GetAllTaskQueues() {
		config := buildWorkerConfig(taskQueue, workflowTrackingActivities, planActivities, prepareEventsActivities, taskActivities, taskActivity, scheduledTaskActivity, exportActivity, hubspotDealSyncActivities, hubspotInvoiceSyncActivities, hubspotQuoteSyncActivities, qbPriceSyncActivities, nomodInvoiceSyncActivities, nomodCustomerSyncActivities, whopInvoiceSyncActivities, moyasarInvoiceSyncActivities, paddleInvoiceSyncActivities, paddleCustomerSyncActivities, paddleSubscriptionSyncActivities, stripeInvoiceSyncActivities, stripeCustomerSyncActivities, razorpayInvoiceSyncActivities, razorpayCustomerSyncActivities, chargebeeInvoiceSyncActivities, chargebeeCustomerSyncActivities, qbInvoiceSyncActivities, qbCustomerSyncActivities, zohoInvoiceSyncActivities, tabsInvoiceSyncActivities, customerActivities, scheduleBillingActivities, billingActivities, invoiceActs, reprocessEventsActivities, reprocessRawEventsActivities, envActivities, cronBundle, alertActs)
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
	nomodCustomerSyncActivities *nomodActivities.CustomerSyncActivities,
	whopInvoiceSyncActivities *whopActivities.InvoiceSyncActivities,
	moyasarInvoiceSyncActivities *moyasarActivities.InvoiceSyncActivities,
	paddleInvoiceSyncActivities *paddleActivities.InvoiceSyncActivities,
	paddleCustomerSyncActivities *paddleActivities.CustomerSyncActivities,
	paddleSubscriptionSyncActivities *paddleActivities.SubscriptionSyncActivities,
	stripeInvoiceSyncActivities *stripeActivities.InvoiceSyncActivities,
	stripeCustomerSyncActivities *stripeActivities.CustomerSyncActivities,
	razorpayInvoiceSyncActivities *razorpayActivities.InvoiceSyncActivities,
	razorpayCustomerSyncActivities *razorpayActivities.CustomerSyncActivities,
	chargebeeInvoiceSyncActivities *chargebeeActivities.InvoiceSyncActivities,
	chargebeeCustomerSyncActivities *chargebeeActivities.CustomerSyncActivities,
	qbInvoiceSyncActivities *qbActivities.QuickBooksInvoiceSyncActivities,
	qbCustomerSyncActivities *qbActivities.QuickBooksCustomerSyncActivities,
	zohoInvoiceSyncActivities *zohoActivities.InvoiceSyncActivities,
	tabsInvoiceSyncActivities *tabsActivities.InvoiceSyncActivities,
	customerActivities *customerActivities.CustomerActivities,
	scheduleBillingActivities *subscriptionActivities.SubscriptionActivities,
	billingActivities *subscriptionActivities.BillingActivities,
	invoiceActs *invoiceActivities.InvoiceActivities,
	reprocessEventsActivities *eventsActivities.ReprocessEventsActivities,
	reprocessRawEventsActivities *eventsActivities.ReprocessRawEventsActivities,
	envActivities *environmentActivities.EnvironmentActivities,
	cron *cronActivityBundle,
	alertActs *alertActivities.AlertActivities,
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
			workflows.WhopInvoiceSyncWorkflow,
			workflows.WhopInvoiceMarkPaidWorkflow,
			workflows.MoyasarInvoiceSyncWorkflow,
			workflows.PaddleInvoiceSyncWorkflow,
			workflows.PaddleInvoicePullSyncWorkflow,
			workflows.StripeInvoiceSyncWorkflow,
			workflows.RazorpayInvoiceSyncWorkflow,
			workflows.ChargebeeInvoiceSyncWorkflow,
			workflows.QuickBooksInvoiceSyncWorkflow,
			workflows.ZohoBooksInvoiceSyncWorkflow,
			workflows.TabsInvoiceSyncWorkflow,
			workflows.StripeCustomerSyncWorkflow,
			workflows.RazorpayCustomerSyncWorkflow,
			workflows.ChargebeeCustomerSyncWorkflow,
			workflows.QuickBooksCustomerSyncWorkflow,
			workflows.NomodCustomerSyncWorkflow,
			workflows.PaddleCustomerSyncWorkflow,
			workflows.PaddleSubscriptionSyncWorkflow,
			workflows.PrepareProcessedEventsWorkflow,
		)
		activitiesList = append(activitiesList,
			taskActivities.ProcessTask,
			hubspotDealSyncActivities.CreateLineItems,
			hubspotDealSyncActivities.UpdateDealAmount,
			hubspotInvoiceSyncActivities.SyncInvoiceToHubSpot,
			hubspotQuoteSyncActivities.CreateQuoteAndLineItems,
			nomodInvoiceSyncActivities.SyncInvoiceToNomod,
			whopInvoiceSyncActivities.SyncInvoiceToWhop,
			whopInvoiceSyncActivities.MarkWhopInvoicePaid,
			moyasarInvoiceSyncActivities.SyncInvoiceToMoyasar,
			paddleInvoiceSyncActivities.SyncInvoiceToPaddle,
			paddleInvoiceSyncActivities.PullAndUpdatePaddleInvoice,
			stripeInvoiceSyncActivities.SyncInvoiceToStripe,
			razorpayInvoiceSyncActivities.SyncInvoiceToRazorpay,
			chargebeeInvoiceSyncActivities.SyncInvoiceToChargebee,
			qbInvoiceSyncActivities.SyncInvoiceToQuickBooks,
			zohoInvoiceSyncActivities.SyncInvoiceToZoho,
			tabsInvoiceSyncActivities.SyncInvoiceToTabs,
			stripeCustomerSyncActivities.SyncCustomerToStripe,
			razorpayCustomerSyncActivities.SyncCustomerToRazorpay,
			chargebeeCustomerSyncActivities.SyncCustomerToChargebee,
			qbCustomerSyncActivities.SyncCustomerToQuickBooks,
			nomodCustomerSyncActivities.SyncCustomerToNomod,
			paddleCustomerSyncActivities.SyncCustomerToPaddle,
			paddleCustomerSyncActivities.EnsureCustomerSyncedToPaddle,
			paddleSubscriptionSyncActivities.SyncSubscriptionToPaddle,
			paddleSubscriptionSyncActivities.CheckSubscriptionSyncStatus,
			prepareEventsActivities.CreateFeatureAndPriceActivity,
			prepareEventsActivities.RolloutToSubscriptionsActivity,
		)

	case types.TemporalTaskQueuePrice:
		workflowsList = append(workflowsList,
			workflows.PriceSyncWorkflow,
			workflows.PriceSyncV2Workflow,
			workflows.QuickBooksPriceSyncWorkflow,
		)
		activitiesList = append(activitiesList,
			planActivities.SyncPlanPrices,
			planActivities.SyncPlanPricesV2,
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
			invoiceWorkflows.ComputeInvoiceWorkflow,
			invoiceWorkflows.DraftAndComputeSubscriptionInvoiceWorkflow,
		)
		activitiesList = append(activitiesList,
			// Invoice workflow activities
			invoiceActs.ComputeInvoiceActivity,
			invoiceActs.CreateDraftForCurrentSubscriptionPeriodActivity,
			invoiceActs.FinalizeInvoiceActivity,
			// invoiceActs.SyncInvoiceToVendorActivity, // Disabled: FinalizeInvoice publishes
			// WebhookEventInvoiceUpdateFinalized; the integration consumer dispatches sync
			// workflows per-provider, so running this activity would duplicate the sync.
			invoiceActs.AttemptInvoicePaymentActivity,
			// Draft finalization cron activity
			invoiceActs.FinalizeDueDraftsActivity,
		)

	case types.TemporalTaskQueueWorkflows:
		// Customer workflows
		workflowsList = append(workflowsList,
			workflows.CustomerOnboardingWorkflow,
			workflows.PrepareProcessedEventsWorkflow,
			workflows.EnvironmentCloneWorkflow,
			workflows.MeterUsageAlertWorkflow,
		)
		// Customer activities
		activitiesList = append(activitiesList,
			customerActivities.CreateCustomerActivity,
			customerActivities.CreateWalletActivity,
			customerActivities.CreateSubscriptionActivity,
			prepareEventsActivities.CreateFeatureAndPriceActivity,
			prepareEventsActivities.RolloutToSubscriptionsActivity,
			planActivities.SyncPlanPrices,
			envActivities.CloneEnvironmentFeatures,
			envActivities.CloneEnvironmentPlans,
			alertActs.CheckSpendBreachActivity,
			alertActs.CheckWalletBalanceActivity,
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

	case types.TemporalTaskQueueCron:
		workflowsList = append(workflowsList,
			cronWorkflows.CreditGrantProcessingWorkflow,
			cronWorkflows.SubscriptionAutoCancellationWorkflow,
			cronWorkflows.WalletCreditExpiryWorkflow,
			cronWorkflows.SubscriptionBillingPeriodsWorkflow,
			cronWorkflows.SubscriptionRenewalDueAlertsWorkflow,
			cronWorkflows.SubscriptionTrialEndDueWorkflow,
			cronWorkflows.OutboundWebhookStaleRetryWorkflow,
			cronWorkflows.AutoInvoiceThresholdBillingWorkflow,
			cronWorkflows.PaddleInvoicePullSyncCronWorkflow,
			cronWorkflows.MoyasarAuthPaymentSettlementWorkflow,
			cronWorkflows.CheckoutSessionExpiryWorkflow,
			cronWorkflows.MarketplaceUsageSnapshotWorkflow,
			cronWorkflows.MarketplaceUsageReportWorkflow,
		)
		activitiesList = append(activitiesList,
			cron.creditGrant.ProcessScheduledCreditGrantApplicationsActivity,
			cron.subscription.ProcessAutoCancellationActivity,
			cron.walletCreditExpiry.ExpireCreditsActivity,
			cron.subscription.UpdateBillingPeriodsActivity,
			cron.subscription.ProcessRenewalDueAlertsActivity,
			cron.subscription.ProcessTrialEndDueActivity,
			cron.webhookOutboundRetry.RetryStaleOutboundWebhooksActivity,
			cron.subscription.ProcessAutoInvoiceThresholdBillingActivity,
			cron.paddleInvoicePullSync.FetchAndTriggerPaddleInvoicePullSyncActivity,
			cron.moyasarAuthPaymentSettlement.ReconcilePendingAuthPaymentsActivity,
			cron.moyasarAuthPaymentSettlement.VoidOrRefundSucceededAuthPaymentsActivity,
			cron.checkoutSessionExpiry.ExpireCheckoutSessionsActivity,
			cron.marketplaceSnapshot.MarketplaceUsageSnapshotActivity,
			cron.marketplaceReport.MarketplaceUsageReportActivity,
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
