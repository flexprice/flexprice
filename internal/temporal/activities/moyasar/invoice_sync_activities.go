package moyasar

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceSyncActivities handles Moyasar invoice sync activities.
type InvoiceSyncActivities struct {
	invoiceService service.InvoiceService
	logger         *logger.Logger
}

// NewInvoiceSyncActivities creates a new Moyasar invoice sync activities handler.
func NewInvoiceSyncActivities(params service.ServiceParams, logger *logger.Logger) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		invoiceService: service.NewInvoiceService(params),
		logger:         logger,
	}
}

// SyncInvoiceToMoyasar syncs an invoice to Moyasar via the service layer.
//
// If the customer has an active saved payment method (token), the invoice is
// charged directly via token (autopay). The webhook resolves the outcome.
//
// If no active token exists, the invoice is synced as a Moyasar invoice link
// (existing flow) so the customer can pay manually.
func (a *InvoiceSyncActivities) SyncInvoiceToMoyasar(ctx context.Context, input models.MoyasarInvoiceSyncWorkflowInput) error {
	a.logger.Info(ctx, "syncing invoice to Moyasar",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	if err := a.invoiceService.SyncInvoiceToMoyasarIfEnabled(ctx, input.InvoiceID); err != nil {
		a.logger.Error(ctx, "failed to sync invoice to Moyasar",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	a.logger.Info(ctx, "successfully synced invoice to Moyasar",
		"invoice_id", input.InvoiceID)

	return nil
}
