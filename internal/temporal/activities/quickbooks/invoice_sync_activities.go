package activities

import (
	"context"

	"github.com/flexprice/flexprice/internal/ee/service"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/activities/billingentity"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// QuickBooksInvoiceSyncActivities handles QuickBooks invoice sync activities.
type QuickBooksInvoiceSyncActivities struct {
	invoiceService service.InvoiceService
	entityScope    interfaces.BillingEntityScope
	logger         *logger.Logger
}

// NewQuickBooksInvoiceSyncActivities creates a new QuickBooks invoice sync activities handler.
func NewQuickBooksInvoiceSyncActivities(params service.ServiceParams, entityScope interfaces.BillingEntityScope, logger *logger.Logger) *QuickBooksInvoiceSyncActivities {
	return &QuickBooksInvoiceSyncActivities{
		invoiceService: service.NewInvoiceService(params),
		entityScope:    entityScope,
		logger:         logger,
	}
}

// SyncInvoiceToQuickBooks syncs an invoice to QuickBooks via the service layer.
func (a *QuickBooksInvoiceSyncActivities) SyncInvoiceToQuickBooks(ctx context.Context, input models.QuickBooksInvoiceSyncWorkflowInput) error {
	a.logger.Info(ctx, "syncing invoice to QuickBooks",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID,
		"tenant_id", input.TenantID,
		"environment_id", input.EnvironmentID)

	ctx = types.SetTenantID(ctx, input.TenantID)
	ctx = types.SetEnvironmentID(ctx, input.EnvironmentID)

	ctx, err := billingentity.ScopeActivity(ctx, a.entityScope, input.CustomerID, input.InvoiceID, "")
	if err != nil {
		return err
	}

	if err := a.invoiceService.SyncInvoiceToQuickBooksIfEnabled(ctx, input.InvoiceID); err != nil {
		a.logger.Error(ctx, "failed to sync invoice to QuickBooks",
			"error", err,
			"invoice_id", input.InvoiceID,
			"customer_id", input.CustomerID)
		return err
	}

	a.logger.Info(ctx, "successfully synced invoice to QuickBooks",
		"invoice_id", input.InvoiceID,
		"customer_id", input.CustomerID)

	return nil
}
