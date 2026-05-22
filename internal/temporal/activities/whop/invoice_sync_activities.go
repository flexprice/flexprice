package whop

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	integrationwhop "github.com/flexprice/flexprice/internal/integration/whop"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

// InvoiceSyncActivities handles Whop invoice sync activities
type InvoiceSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	logger             *logger.Logger
}

// NewInvoiceSyncActivities creates a new Whop invoice sync activities handler
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

// MarkWhopInvoicePaid marks the corresponding Whop invoice as paid when Flexprice marks it paid
func (a *InvoiceSyncActivities) MarkWhopInvoicePaid(
	ctx context.Context,
	input models.WhopInvoiceMarkPaidWorkflowInput,
) error {
	a.logger.Infow("marking Whop invoice as paid",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	whopIntegration, err := a.integrationFactory.GetWhopIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			return temporal.NewNonRetryableApplicationError(
				"Whop connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		return err
	}

	if err := whopIntegration.InvoiceSyncSvc.MarkInvoicePaidInWhop(ctx, input.InvoiceID); err != nil {
		a.logger.Errorw("failed to mark Whop invoice as paid",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	return nil
}

// SyncInvoiceToWhop syncs a Flexprice invoice to Whop
func (a *InvoiceSyncActivities) SyncInvoiceToWhop(
	ctx context.Context,
	input models.WhopInvoiceSyncWorkflowInput,
) error {
	a.logger.Infow("syncing invoice to Whop",
		"invoice_id", input.InvoiceID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	whopIntegration, err := a.integrationFactory.GetWhopIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Debugw("Whop connection not configured",
				"invoice_id", input.InvoiceID)
			return temporal.NewNonRetryableApplicationError(
				"Whop connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Errorw("failed to get Whop integration",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	syncReq := integrationwhop.WhopInvoiceSyncRequest{
		InvoiceID: input.InvoiceID,
	}

	_, err = whopIntegration.InvoiceSyncSvc.SyncInvoiceToWhop(ctx, syncReq, a.customerService)
	if err != nil {
		a.logger.Errorw("failed to sync invoice to Whop",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	a.logger.Infow("successfully synced invoice to Whop",
		"invoice_id", input.InvoiceID)

	return nil
}
