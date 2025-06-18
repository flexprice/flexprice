package temporal

import (
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/integration"
	"github.com/flexprice/flexprice/internal/domain/stripe"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/activities"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"go.temporal.io/sdk/worker"
)

// RegisterWorkflowsAndActivities registers all workflows and activities with a Temporal worker.
func RegisterWorkflowsAndActivities(w worker.Worker) {
	w.RegisterWorkflow(workflows.CronBillingWorkflow)
	w.RegisterWorkflow(workflows.CalculateChargesWorkflow)
	w.RegisterWorkflow(workflows.StripeEventSyncWorkflow)
	w.RegisterWorkflow(workflows.CronStripeEventSyncWorkflow)
	w.RegisterWorkflow(workflows.ManualStripeEventSyncWorkflow)
	w.RegisterActivity(&activities.BillingActivities{})
}

// RegisterStripeSyncActivities registers Stripe sync activities with dependencies
func RegisterStripeSyncActivities(
	w worker.Worker,
	processedEventRepo events.ProcessedEventRepository,
	customerMappingRepo integration.EntityIntegrationMappingRepository,
	stripeSyncBatchRepo integration.StripeSyncBatchRepository,
	stripeTenantConfigRepo integration.StripeTenantConfigRepository,
	meterProviderMappingRepo integration.MeterProviderMappingRepository,
	stripeClient stripe.Client,
	logger *logger.Logger,
) {
	stripeSyncActivities := activities.NewStripeSyncActivities(
		processedEventRepo,
		customerMappingRepo,
		stripeSyncBatchRepo,
		stripeTenantConfigRepo,
		meterProviderMappingRepo,
		stripeClient,
		logger,
	)
	w.RegisterActivity(stripeSyncActivities)
}
