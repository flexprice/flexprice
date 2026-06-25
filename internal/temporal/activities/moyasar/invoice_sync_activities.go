package moyasar

import (
	"context"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/integration"
	moyasarintegration "github.com/flexprice/flexprice/internal/integration/moyasar"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
)

// InvoiceSyncActivities handles Moyasar invoice sync activities
type InvoiceSyncActivities struct {
	integrationFactory *integration.Factory
	customerService    interfaces.CustomerService
	invoiceService     interfaces.InvoiceService
	logger             *logger.Logger
}

// NewInvoiceSyncActivities creates a new Moyasar invoice sync activities handler
func NewInvoiceSyncActivities(
	integrationFactory *integration.Factory,
	customerService interfaces.CustomerService,
	invoiceService interfaces.InvoiceService,
	logger *logger.Logger,
) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		integrationFactory: integrationFactory,
		customerService:    customerService,
		invoiceService:     invoiceService,
		logger:             logger,
	}
}

// SyncInvoiceToMoyasar syncs an invoice to Moyasar.
//
// If the customer has an active saved payment method (token), the invoice is
// charged directly via token (autopay). The webhook resolves the outcome.
//
// If no active token exists, the invoice is synced as a Moyasar invoice link
// (existing flow) so the customer can pay manually.
func (a *InvoiceSyncActivities) SyncInvoiceToMoyasar(
	ctx context.Context,
	input models.MoyasarInvoiceSyncWorkflowInput,
) error {
	a.logger.Info(ctx, "syncing invoice to Moyasar",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	moyasarIntegration, err := a.integrationFactory.GetMoyasarIntegration(ctx)
	if err != nil {
		if ierr.IsNotFound(err) {
			a.logger.Debug(ctx, "Moyasar connection not configured",
				"invoice_id", input.InvoiceID,
				"customer_id", input.CustomerID)
			return temporal.NewNonRetryableApplicationError(
				"Moyasar connection not configured",
				"ConnectionNotFound",
				err,
			)
		}
		a.logger.Error(ctx, "failed to get Moyasar integration",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	// Step 1: always sync invoice to Moyasar to get/create the Moyasar invoice ID
	syncResp, err := moyasarIntegration.InvoiceSyncSvc.SyncInvoiceToMoyasar(
		ctx,
		moyasarintegration.MoyasarInvoiceSyncRequest{InvoiceID: input.InvoiceID},
		a.customerService,
	)
	if err != nil {
		a.logger.Error(ctx, "failed to sync invoice to Moyasar",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}

	a.logger.Info(ctx, "invoice synced to Moyasar",
		"invoice_id", input.InvoiceID,
		"moyasar_invoice_id", syncResp.MoyasarInvoiceID)

	// Step 2: attempt autopay via saved token, passing the Moyasar invoice ID
	charged, err := a.chargeWithTokenIfAvailable(ctx, moyasarIntegration, input, syncResp.MoyasarInvoiceID)
	if err != nil {
		a.logger.Error(ctx, "failed to charge invoice with saved token",
			"error", err,
			"invoice_id", input.InvoiceID)
		return err
	}
	if charged {
		a.logger.Info(ctx, "invoice charged via saved token, webhook will confirm",
			"invoice_id", input.InvoiceID,
			"moyasar_invoice_id", syncResp.MoyasarInvoiceID)
		return nil
	}

	// No active token — invoice link already created in step 1, customer pays manually
	a.logger.Info(ctx, "no active payment method, invoice-link flow ready",
		"invoice_id", input.InvoiceID,
		"moyasar_invoice_id", syncResp.MoyasarInvoiceID)

	return nil
}

// chargeWithTokenIfAvailable fetches the invoice and attempts autopay via a saved token.
// Returns (true, nil) if charged, (false, nil) if no token available, (false, err) on error.
func (a *InvoiceSyncActivities) chargeWithTokenIfAvailable(
	ctx context.Context,
	m *integration.MoyasarIntegration,
	input models.MoyasarInvoiceSyncWorkflowInput,
	moyasarInvoiceID string,
) (bool, error) {
	inv, err := a.invoiceService.GetInvoice(ctx, input.InvoiceID)
	if err != nil {
		return false, err
	}

	if inv.AmountDue.IsZero() {
		a.logger.Info(ctx, "invoice amount is zero, skipping autopay",
			"invoice_id", input.InvoiceID)
		return false, nil
	}

	customerID := input.CustomerID
	if customerID == "" {
		customerID = inv.CustomerID
	}

	return m.ChargeInvoiceWithToken(ctx, input.InvoiceID, customerID, inv.AmountDue, inv.Currency, moyasarInvoiceID)
}
