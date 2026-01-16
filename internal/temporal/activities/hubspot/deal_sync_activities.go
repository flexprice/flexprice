package hubspot

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

// DealSyncActivities contains all HubSpot deal sync activities
type DealSyncActivities struct {
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

// NewDealSyncActivities creates a new instance of DealSyncActivities
func NewDealSyncActivities(
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *DealSyncActivities {
	return &DealSyncActivities{
		integrationFactory: integrationFactory,
		logger:             logger,
	}
}

// CreateLineItems creates HubSpot line items from subscription
// This is the first step - it creates line items but doesn't update deal amount
func (a *DealSyncActivities) CreateLineItems(
	ctx context.Context,
	input models.HubSpotDealSyncWorkflowInput,
) error {
	a.logger.Infow("creating HubSpot line items",
		"subscription_id", input.SubscriptionID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Set context for operations
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get HubSpot integration with proper context
	hubspotIntegration, err := a.integrationFactory.GetHubSpotIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Debugw("HubSpot connection not configured",
				"subscription_id", input.SubscriptionID)
			// Return NON-RETRYABLE error - connection doesn't exist, retrying won't help
			return temporal.NewNonRetryableApplicationError(
				"HubSpot connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get HubSpot integration",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	// Create line items - uses existing DealSyncService logic
	err = hubspotIntegration.DealSyncSvc.SyncSubscriptionToDeal(ctx, input.SubscriptionID)
	if err != nil {
		a.logger.Errorw("failed to create line items",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	a.logger.Infow("successfully created HubSpot line items",
		"subscription_id", input.SubscriptionID)

	return nil
}

// UpdateDealAmount updates the deal amount based on HubSpot's calculated ACV
// This is the second step - called after sleep to allow HubSpot to recalculate ACV
func (a *DealSyncActivities) UpdateDealAmount(
	ctx context.Context,
	input models.HubSpotDealSyncWorkflowInput,
) error {
	a.logger.Infow("updating HubSpot deal amount",
		"customer_id", input.CustomerID,
		"deal_id", input.DealID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Set context for operations
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get HubSpot integration with proper context
	hubspotIntegration, err := a.integrationFactory.GetHubSpotIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Debugw("HubSpot connection not configured",
				"customer_id", input.CustomerID,
				"deal_id", input.DealID)
			// Return NON-RETRYABLE error - connection doesn't exist, retrying won't help
			return temporal.NewNonRetryableApplicationError(
				"HubSpot connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get HubSpot integration",
			"error", err,
			"customer_id", input.CustomerID,
			"deal_id", input.DealID)
		return err
	}

	// Update deal amount - uses existing DealSyncService logic
	// Now we pass customerID and dealID directly instead of fetching subscription
	err = hubspotIntegration.DealSyncSvc.UpdateDealAmountFromACV(ctx, input.CustomerID, input.DealID)
	if err != nil {
		a.logger.Errorw("failed to update deal amount",
			"error", err,
			"customer_id", input.CustomerID,
			"deal_id", input.DealID)
		return err
	}

	a.logger.Infow("successfully updated HubSpot deal amount",
		"customer_id", input.CustomerID,
		"deal_id", input.DealID)

	return nil
}
