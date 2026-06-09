package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/logger"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// PaddleInvoicePullSyncActivities runs the scheduled Paddle invoice pull-sync fan-out.
type PaddleInvoicePullSyncActivities struct {
	invoiceRepo invoice.Repository
	temporalSvc temporalservice.TemporalService
	logger      *logger.Logger
}

// NewPaddleInvoicePullSyncActivities constructs PaddleInvoicePullSyncActivities.
func NewPaddleInvoicePullSyncActivities(
	invoiceRepo invoice.Repository,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
) *PaddleInvoicePullSyncActivities {
	return &PaddleInvoicePullSyncActivities{
		invoiceRepo: invoiceRepo,
		temporalSvc: temporalSvc,
		logger:      log,
	}
}

// FetchAndTriggerPaddleInvoicePullSyncActivity fetches all finalized+unpaid invoices for
// tenant/env pairs with an active Paddle connection and triggers a
// PaddleInvoicePullSyncWorkflow for each.
func (a *PaddleInvoicePullSyncActivities) FetchAndTriggerPaddleInvoicePullSyncActivity(
	ctx context.Context,
) (*cronModels.PaddleInvoicePullSyncCronResult, error) {
	a.logger.Info(ctx, "starting Paddle invoice pull-sync cron activity")

	invoices, err := a.invoiceRepo.ListPendingInvoicesByProvider(ctx, types.SecretProviderPaddle)
	if err != nil {
		a.logger.Error(ctx, "failed to fetch Paddle pending invoices", "error", err)
		return nil, err
	}

	a.logger.Info(ctx, "fetched Paddle pending invoices", "count", len(invoices))

	result := &cronModels.PaddleInvoicePullSyncCronResult{}
	for _, inv := range invoices {
		result.Total++

		envCtx := types.SetTenantID(ctx, inv.TenantID)
		envCtx = types.SetEnvironmentID(envCtx, inv.EnvironmentID)

		input := cronModels.PaddleInvoicePullSyncWorkflowInput{
			InvoiceID:     inv.InvoiceID,
			TenantID:      inv.TenantID,
			EnvironmentID: inv.EnvironmentID,
		}
		_, wErr := a.temporalSvc.ExecuteWorkflow(envCtx, types.TemporalPaddleInvoicePullSyncWorkflow, input)
		if wErr != nil {
			a.logger.Error(ctx, "failed to trigger Paddle invoice pull-sync workflow",
				"invoice_id", inv.InvoiceID,
				"tenant_id", inv.TenantID,
				"environment_id", inv.EnvironmentID,
				"error", wErr)
			result.Failed++
			continue
		}
		result.Triggered++
	}

	a.logger.Info(ctx, "completed Paddle invoice pull-sync cron activity",
		"total", result.Total, "triggered", result.Triggered, "failed", result.Failed)

	return result, nil
}
