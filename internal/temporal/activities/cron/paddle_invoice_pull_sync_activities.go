package cron

import (
	"context"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
)

// PaddleInvoicePullSyncActivities runs the scheduled Paddle invoice pull-sync fan-out.
type PaddleInvoicePullSyncActivities struct {
	db          postgres.IClient
	temporalSvc temporalservice.TemporalService
	logger      *logger.Logger
}

// NewPaddleInvoicePullSyncActivities constructs PaddleInvoicePullSyncActivities.
func NewPaddleInvoicePullSyncActivities(
	db postgres.IClient,
	temporalSvc temporalservice.TemporalService,
	log *logger.Logger,
) *PaddleInvoicePullSyncActivities {
	return &PaddleInvoicePullSyncActivities{
		db:          db,
		temporalSvc: temporalSvc,
		logger:      log,
	}
}

type paddleInvoiceRow struct {
	invoiceID     string
	tenantID      string
	environmentID string
}

// FetchAndTriggerPaddleInvoicePullSyncActivity fetches all finalized+unpaid invoices for
// tenant/env pairs with an active Paddle connection and triggers a
// PaddleInvoicePullSyncWorkflow for each.
func (a *PaddleInvoicePullSyncActivities) FetchAndTriggerPaddleInvoicePullSyncActivity(
	ctx context.Context,
) (*cronModels.PaddleInvoicePullSyncCronResult, error) {
	a.logger.Infow("starting Paddle invoice pull-sync cron activity")

	invoices, err := a.fetchPaddlePendingInvoices(ctx)
	if err != nil {
		a.logger.Errorw("failed to fetch Paddle pending invoices", "error", err)
		return nil, err
	}

	a.logger.Infow("fetched Paddle pending invoices", "count", len(invoices))

	result := &cronModels.PaddleInvoicePullSyncCronResult{}
	for _, inv := range invoices {
		result.Total++

		envCtx := types.SetTenantID(ctx, inv.tenantID)
		envCtx = types.SetEnvironmentID(envCtx, inv.environmentID)

		input := cronModels.PaddleInvoicePullSyncWorkflowInput{
			InvoiceID:     inv.invoiceID,
			TenantID:      inv.tenantID,
			EnvironmentID: inv.environmentID,
		}
		_, wErr := a.temporalSvc.ExecuteWorkflow(envCtx, types.TemporalPaddleInvoicePullSyncWorkflow, input)
		if wErr != nil {
			a.logger.Errorw("failed to trigger Paddle invoice pull-sync workflow",
				"invoice_id", inv.invoiceID,
				"tenant_id", inv.tenantID,
				"environment_id", inv.environmentID,
				"error", wErr)
			result.Failed++
			continue
		}
		result.Triggered++
	}

	a.logger.Infow("completed Paddle invoice pull-sync cron activity",
		"total", result.Total, "triggered", result.Triggered, "failed", result.Failed)

	return result, nil
}

// fetchPaddlePendingInvoices returns all finalized+unpaid invoices belonging to tenant/env
// pairs that have an active Paddle connection, via a single JOIN query.
func (a *PaddleInvoicePullSyncActivities) fetchPaddlePendingInvoices(ctx context.Context) ([]paddleInvoiceRow, error) {
	const query = `
		SELECT i.id, i.tenant_id, i.environment_id
		FROM invoices i
		INNER JOIN connections c
			ON  c.tenant_id      = i.tenant_id
			AND c.environment_id = i.environment_id
		WHERE c.provider_type  = $1
		  AND c.status         = 'published'
		  AND i.invoice_status = $2
		  AND i.payment_status = $3
		  AND i.status         = 'published'`

	rows, err := a.db.Reader(ctx).QueryContext(ctx, query,
		string(types.SecretProviderPaddle),
		string(types.InvoiceStatusFinalized),
		string(types.PaymentStatusPending),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []paddleInvoiceRow
	for rows.Next() {
		var row paddleInvoiceRow
		if err := rows.Scan(&row.invoiceID, &row.tenantID, &row.environmentID); err != nil {
			return nil, err
		}
		result = append(result, row)
	}
	return result, rows.Err()
}
