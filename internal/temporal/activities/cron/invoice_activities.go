package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/activity"
)

// InvoiceCronActivities wraps invoice-related cron jobs.
type InvoiceCronActivities struct {
	invoiceService service.InvoiceService
	logger         *logger.Logger
}

// NewInvoiceCronActivities builds activities for invoice cron workflows.
func NewInvoiceCronActivities(invoiceService service.InvoiceService, log *logger.Logger) *InvoiceCronActivities {
	return &InvoiceCronActivities{
		invoiceService: invoiceService,
		logger:         log,
	}
}

// VoidOldPendingInvoicesActivity voids old pending invoices for incomplete subscriptions.
func (a *InvoiceCronActivities) VoidOldPendingInvoicesActivity(ctx context.Context) (*cronModels.VoidOldPendingInvoicesWorkflowResult, error) {
	log := activity.GetLogger(ctx)
	log.Info("Starting void-old-pending-invoices activity")

	if err := a.invoiceService.VoidOldPendingInvoices(ctx); err != nil {
		return nil, err
	}

	log.Info("Completed void-old-pending-invoices activity")
	return &cronModels.VoidOldPendingInvoicesWorkflowResult{}, nil
}
