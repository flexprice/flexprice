package hubspot

import (
	"context"

	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/activities/billingentity"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// QuoteSyncActivities contains all HubSpot quote sync activities
type QuoteSyncActivities struct {
	integrationFactory *integration.Factory
	entityScope        interfaces.BillingEntityScope
	logger             *logger.Logger
}

// NewQuoteSyncActivities creates a new instance of QuoteSyncActivities
func NewQuoteSyncActivities(
	integrationFactory *integration.Factory,
	entityScope interfaces.BillingEntityScope,
	logger *logger.Logger,
) *QuoteSyncActivities {
	return &QuoteSyncActivities{
		integrationFactory: integrationFactory,
		entityScope:        entityScope,
		logger:             logger,
	}
}

// CreateQuoteAndLineItems creates HubSpot quote from subscription and attaches line items
func (a *QuoteSyncActivities) CreateQuoteAndLineItems(
	ctx context.Context,
	input models.HubSpotQuoteSyncWorkflowInput,
) error {
	a.logger.Info(ctx, "creating HubSpot quote and line items",
		"subscription_id", input.SubscriptionID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Set context for operations
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	ctx, err := billingentity.ScopeActivity(ctx, a.entityScope, input.CustomerID, "", input.SubscriptionID)
	if err != nil {
		return err
	}

	// Get HubSpot integration with proper context
	hubspotIntegration, err := a.integrationFactory.GetHubSpotIntegration(ctx)
	if err != nil {
		a.logger.Error(ctx, "failed to get HubSpot integration",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	// Create quote and line items - uses existing QuoteSyncService logic
	err = hubspotIntegration.QuoteSyncSvc.SyncSubscriptionToQuote(ctx, input.SubscriptionID)
	if err != nil {
		a.logger.Error(ctx, "failed to create quote and line items",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	a.logger.Info(ctx, "successfully created HubSpot quote and line items",
		"subscription_id", input.SubscriptionID)

	return nil
}
