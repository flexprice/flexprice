package razorpay

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceSyncActivities handles Razorpay invoice sync activities.
type InvoiceSyncActivities struct {
	invoiceService service.InvoiceService
	logger         *logger.Logger
}

// NewInvoiceSyncActivities creates a new Razorpay invoice sync activities handler.
func NewInvoiceSyncActivities(params service.ServiceParams, logger *logger.Logger) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		invoiceService: service.NewInvoiceService(params),
		logger:         logger,
	}
}

// SyncInvoiceToRazorpay syncs an invoice to Razorpay via the service layer.
func (a *InvoiceSyncActivities) SyncInvoiceToRazorpay(ctx context.Context, input models.RazorpayInvoiceSyncWorkflowInput) error {
	a.logger.Infow("syncing invoice to Razorpay",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	if err := a.invoiceService.SyncInvoiceToRazorpayIfEnabled(ctx, input.InvoiceID); err != nil {
		a.logger.Errorw("failed to sync invoice to Razorpay",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	a.logger.Infow("successfully synced invoice to Razorpay",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
