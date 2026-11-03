package ent

import (
	"context"
	"time"

	entEnvironment "github.com/flexprice/flexprice/ent/environment"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// selfBillingInvoiceSumQuery reads across every tenant on purpose: Flexprice bills each tenant on its invoice value.
const selfBillingInvoiceSumQuery = `
	SELECT
		tenant_id,
		environment_id,
		currency,
		date_trunc('hour', finalized_at AT TIME ZONE 'UTC') AS hour,
		COUNT(*) AS invoice_count,
		COALESCE(SUM(subtotal), 0),
		COALESCE(SUM(total_discount), 0),
		COALESCE(SUM(total_prepaid_credits_applied), 0),
		COALESCE(SUM(total_tax), 0),
		COALESCE(SUM(total), 0),
		COALESCE(SUM(amount_due), 0),
		COALESCE(SUM(amount_paid), 0)
	FROM invoices
	WHERE status = $1
		AND invoice_status = $2
		AND finalized_at >= $3
		AND finalized_at < $4
	GROUP BY tenant_id, environment_id, currency, hour
`

type selfBillingRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

func NewSelfBillingRepository(client postgres.IClient, logger *logger.Logger) domainsb.PostgresRepository {
	return &selfBillingRepository{
		client: client,
		logger: logger,
	}
}

func (r *selfBillingRepository) SumInvoicesByTenantHour(ctx context.Context, from, to time.Time) ([]*domainsb.TenantHourInvoices, error) {
	span := StartRepositorySpan(ctx, "selfbilling", "sum_invoices_by_tenant_hour", map[string]interface{}{
		"from": from,
		"to":   to,
	})
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).QueryContext(ctx, selfBillingInvoiceSumQuery,
		string(types.StatusPublished),
		string(types.InvoiceStatusFinalized),
		from.UTC(),
		to.UTC(),
	)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to sum invoices for self-billing").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var out []*domainsb.TenantHourInvoices
	for rows.Next() {
		row := &domainsb.TenantHourInvoices{}
		if err := rows.Scan(
			&row.TenantID,
			&row.EnvironmentID,
			&row.Currency,
			&row.Hour,
			&row.InvoiceCount,
			&row.Subtotal,
			&row.TotalDiscount,
			&row.TotalPrepaidCreditsApplied,
			&row.TotalTax,
			&row.Total,
			&row.AmountDue,
			&row.AmountPaid,
		); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to read self-billing invoice sums").
				Mark(ierr.ErrDatabase)
		}

		row.Hour = row.Hour.UTC()
		out = append(out, row)
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to read self-billing invoice sums").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return out, nil
}

// ListEnvironmentTypes reads across every tenant on purpose: usage rows carry only the environment ID.
func (r *selfBillingRepository) ListEnvironmentTypes(ctx context.Context, environmentIDs []string) (map[string]types.EnvironmentType, error) {
	out := make(map[string]types.EnvironmentType, len(environmentIDs))
	if len(environmentIDs) == 0 {
		return out, nil
	}

	span := StartRepositorySpan(ctx, "selfbilling", "list_environment_types", map[string]interface{}{
		"environment_count": len(environmentIDs),
	})
	defer FinishSpan(span)

	envs, err := r.client.Reader(ctx).Environment.Query().
		Where(entEnvironment.IDIn(environmentIDs...)).
		Select(entEnvironment.FieldID, entEnvironment.FieldType).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to read environment types for self-billing").
			Mark(ierr.ErrDatabase)
	}

	for _, env := range envs {
		out[env.ID] = types.EnvironmentType(env.Type)
	}

	SetSpanSuccess(span)
	return out, nil
}
