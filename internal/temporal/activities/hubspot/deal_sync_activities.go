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

func (a *DealSyncActivities) CreateLineItem(
	ctx context.Context,
	input models.HubSpotDealSyncWorkflowInput,
) error {
	a.logger.Info(ctx, "creating HubSpot line item",
		"subscription_id", input.SubscriptionID,
		"line_item_id", input.LineItemID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Set context for operations
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get HubSpot integration with proper context
	hubspotIntegration, err := a.integrationFactory.GetHubSpotIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(
				"HubSpot connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Error(ctx, "failed to get HubSpot integration",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	err = hubspotIntegration.DealSyncSvc.SyncLineItemCreated(ctx, input.SubscriptionID, input.LineItemID, input.DealID)
	if err != nil {
		a.logger.Error(ctx, "failed to create line item",
			"error", err,
			"subscription_id", input.SubscriptionID,
			"line_item_id", input.LineItemID)
		return err
	}

	return nil
}

func (a *DealSyncActivities) DeleteLineItem(
	ctx context.Context,
	input models.HubSpotDealSyncWorkflowInput,
) error {
	a.logger.Info(ctx, "deleting HubSpot line item",
		"subscription_id", input.SubscriptionID,
		"line_item_id", input.LineItemID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	hubspotIntegration, err := a.integrationFactory.GetHubSpotIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(
				"HubSpot connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Error(ctx, "failed to get HubSpot integration",
			"error", err,
			"subscription_id", input.SubscriptionID)
		return err
	}

	err = hubspotIntegration.DealSyncSvc.SyncLineItemDeleted(ctx, input.LineItemID)
	if err != nil {
		a.logger.Error(ctx, "failed to delete line item",
			"error", err,
			"subscription_id", input.SubscriptionID,
			"line_item_id", input.LineItemID)
		return err
	}

	a.logger.Info(ctx, "successfully deleted HubSpot line item",
		"subscription_id", input.SubscriptionID,
		"line_item_id", input.LineItemID)

	return nil
}

// UpdateDealAmount updates the deal amount based on HubSpot's calculated ACV
// This is the second step - called after sleep to allow HubSpot to recalculate ACV
func (a *DealSyncActivities) UpdateDealAmount(
	ctx context.Context,
	input models.HubSpotDealSyncWorkflowInput,
) error {
	a.logger.Info(ctx, "updating HubSpot deal amount",
		"customer_id", input.CustomerID,
		"deal_id", input.DealID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	hubspotIntegration, err := a.integrationFactory.GetHubSpotIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(
				"HubSpot connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Error(ctx, "failed to get HubSpot integration",
			"error", err,
			"customer_id", input.CustomerID,
			"deal_id", input.DealID)
		return err
	}

	err = hubspotIntegration.DealSyncSvc.UpdateDealAmountFromACV(ctx, input.CustomerID, input.DealID)
	if err != nil {
		a.logger.Error(ctx, "failed to update deal amount",
			"error", err,
			"customer_id", input.CustomerID,
			"deal_id", input.DealID)
		return err
	}

	a.logger.Info(ctx, "successfully updated HubSpot deal amount",
		"customer_id", input.CustomerID,
		"deal_id", input.DealID)

	return nil
}
