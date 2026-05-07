package chargebee

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// InvoiceSyncActivities handles Chargebee invoice sync activities.
type InvoiceSyncActivities struct {
	invoiceService service.InvoiceService
	logger         *logger.Logger
}

// NewInvoiceSyncActivities creates a new Chargebee invoice sync activities handler.
func NewInvoiceSyncActivities(params service.ServiceParams, logger *logger.Logger) *InvoiceSyncActivities {
	return &InvoiceSyncActivities{
		invoiceService: service.NewInvoiceService(params),
		logger:         logger,
	}
}

// SyncInvoiceToChargebee syncs an invoice to Chargebee via the service layer.
func (a *InvoiceSyncActivities) SyncInvoiceToChargebee(ctx context.Context, input models.ChargebeeInvoiceSyncWorkflowInput) error {
	a.logger.Infow("syncing invoice to Chargebee",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	if err := a.invoiceService.SyncInvoiceToChargebeeIfEnabled(ctx, input.InvoiceID); err != nil {
		a.logger.Errorw("failed to sync invoice to Chargebee",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	a.logger.Infow("successfully synced invoice to Chargebee",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
