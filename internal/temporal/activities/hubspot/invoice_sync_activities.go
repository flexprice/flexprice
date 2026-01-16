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

// InvoiceSyncActivities contains all HubSpot invoice sync activities
type InvoiceSyncActivities struct {
	integrationFactory *integration.Factory
	logger             *logger.Logger
}

// NewInvoiceSyncActivities creates a new instance of InvoiceSyncActivities
func NewInvoiceSyncActivities(
	integrationFactory *integration.Factory,
	logger *logger.Logger,
) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		integrationFactory: integrationFactory,
		logger:             logger,
	}
}

// SyncInvoiceToHubSpot syncs a FlexPrice invoice to HubSpot
// This is a thin wrapper around the HubSpot integration service
func (a *InvoiceSyncActivities) SyncInvoiceToHubSpot(
	ctx context.Context,
	input models.HubSpotInvoiceSyncWorkflowInput,
) error {
	a.logger.Infow("syncing invoice to HubSpot",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Set context values for tenant and environment
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get HubSpot integration with runtime context
	hubspotIntegration, err := a.integrationFactory.GetHubSpotIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Debugw("HubSpot connection not configured",
				"invoice_id", input.InvoiceID,
				"customer_id", input.CustomerID)
			// Return NON-RETRYABLE error - connection doesn't exist, retrying won't help
			return temporal.NewNonRetryableApplicationError(
				"HubSpot connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get HubSpot integration",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	// Get HubSpot contact ID for the customer
	hubspotContactID, err := hubspotIntegration.InvoiceSyncSvc.GetHubSpotContactID(ctx, input.CustomerID)
	if err != nil {
		a.logger.Warnw("customer not synced to HubSpot",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	a.logger.Infow("found HubSpot contact for customer",
		"customer_id", input.CustomerID,
		"hubspot_contact_id", hubspotContactID,
		"invoice_id", input.InvoiceID)

	// Perform the sync using the existing service
	err = hubspotIntegration.InvoiceSyncSvc.SyncInvoiceToHubSpot(ctx, input.InvoiceID, hubspotContactID)
	if err != nil {
		a.logger.Errorw("failed to sync invoice to HubSpot",
			"error", err,
			"invoice_id", input.InvoiceID,
			"hubspot_contact_id", hubspotContactID)
		return err
	}

	a.logger.Infow("successfully synced invoice to HubSpot",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"hubspot_contact_id", hubspotContactID)

	return nil
}
