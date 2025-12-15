package nomod

import (
	"context"

	"github.com/flexprice/flexprice/internal/integration"
	"github.com/flexprice/flexprice/internal/integration/nomod"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceSyncActivities handles Nomod invoice sync activities
type InvoiceSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	logger             *logger.Logger
}

// NewInvoiceSyncActivities creates a new Nomod invoice sync activities handler
func NewInvoiceSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	logger *logger.Logger,
) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		logger:             logger,
	}
}

// SyncInvoiceToNomod syncs an invoice to Nomod
// This is a thin wrapper around the Nomod integration service
func (a *InvoiceSyncActivities) SyncInvoiceToNomod(
	ctx context.Context,
	input models.NomodInvoiceSyncWorkflowInput,
) error {
	a.logger.Infow("syncing invoice to Nomod",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	// Set context values for tenant and environment
	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	// Get Nomod integration with runtime context
	nomodIntegration, err := a.integrationFactory.GetNomodIntegration(ctx)
	if err != nil {
		a.logger.Errorw("failed to get Nomod integration",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	// Perform the sync using the existing service
	syncReq := nomod.NomodInvoiceSyncRequest{
		InvoiceID: input.InvoiceID,
	}

	_, err = nomodIntegration.InvoiceSyncSvc.SyncInvoiceToNomod(ctx, syncReq, a.customerService)
	if err != nil {
		a.logger.Errorw("failed to sync invoice to Nomod",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	a.logger.Infow("successfully synced invoice to Nomod",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
