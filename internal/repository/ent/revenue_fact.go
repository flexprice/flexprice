package ent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	entrevenuefact "github.com/flexprice/flexprice/ent/revenuefact"
	"github.com/flexprice/flexprice/internal/domain/revenuefact"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

// revenueFactInsertColumns is the column list of the revenue_facts table in the
// order UpsertProvisional binds its args. Shared by the INSERT column list so
// the placeholders and args never drift apart.
var revenueFactInsertColumns = []string{
	"id", "tenant_id", "environment_id", "customer_id", "subscription_id", "sub_line_item_id", "price_id", "meter_id",
	"aggregation_type", "revenue_source", "period_start", "period_end", "day", "service_start", "service_end",
	"recognition_method", "usage_at_list_rate", "tier_delta", "entitlement_amount", "line_discount", "invoice_discount",
	"net_amount", "billable_qty", "entitlement_qty", "decomposition_mode", "currency", "status", "is_revert",
	"invoice_id", "invoice_line_item_id", "lock_adjusted_day", "computed_at", "version",
}

// revenueFactMutableColumns are re-written on conflict: every column except
// the grain columns, the id, and version (bumped separately).
var revenueFactMutableColumns = []string{
	"customer_id", "sub_line_item_id", "meter_id", "aggregation_type",
	"period_start", "period_end", "service_start", "service_end",
	"recognition_method", "usage_at_list_rate", "tier_delta", "entitlement_amount",
	"line_discount", "invoice_discount", "net_amount", "billable_qty", "entitlement_qty",
	"decomposition_mode", "currency", "status", "is_revert",
	"invoice_id", "invoice_line_item_id", "lock_adjusted_day", "computed_at",
}

// enumStrPtr nil-safely converts a nullable string-enum pointer to a *string so
// the SQL driver binds it consistently with the non-pointer enum args (which are
// cast via string(...)).
func enumStrPtr[T ~string](p *T) *string {
	if p == nil {
		return nil
	}
	s := string(*p)
	return &s
}

var revenueFactInsertColumnList = strings.Join(revenueFactInsertColumns, ", ")

type revenueFactRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

// NewRevenueFactRepository creates an ent-backed revenuefact.Repository.
func NewRevenueFactRepository(client postgres.IClient, log *logger.Logger) revenuefact.Repository {
	return &revenueFactRepository{client: client, log: log}
}

// UpsertProvisional inserts or updates facts on the provisional grain,
// bumping version on conflict. Raw SQL: ent cannot express a partial-index
// ON CONFLICT. Tenant, environment and status always come from ctx, never
// from the input.
func (r *revenueFactRepository) UpsertProvisional(ctx context.Context, facts []*revenuefact.RevenueFact) error {
	if len(facts) == 0 {
		return nil
	}

	// Postgres treats NULLs as unequal in unique indexes, so a NULL price_id
	// or sub_line_item_id would dodge ON CONFLICT and duplicate rows on
	// re-roll — require both.
	for _, f := range facts {
		if f.PriceID == nil || *f.PriceID == "" {
			return ierr.NewError("revenue fact requires a non-empty price_id").
				WithHint("Provisional revenue facts must carry a non-empty price_id").
				Mark(ierr.ErrValidation)
		}
		if f.SubLineItemID == nil || *f.SubLineItemID == "" {
			return ierr.NewError("revenue fact requires a non-empty sub_line_item_id").
				WithHint("Provisional revenue facts must carry a non-empty sub_line_item_id").
				Mark(ierr.ErrValidation)
		}
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	now := time.Now().UTC()

	span := StartRepositorySpan(ctx, "revenue_fact", "upsert_provisional", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
		"fact_count":     len(facts),
	})
	defer FinishSpan(span)

	// Postgres caps a statement at 65535 bind parameters, so upsert in chunks,
	// all in one transaction.
	maxRowsPerStmt := 65535 / len(revenueFactInsertColumns)
	upsertAll := func(ctx context.Context) error {
		for start := 0; start < len(facts); start += maxRowsPerStmt {
			end := min(start+maxRowsPerStmt, len(facts))
			if err := r.upsertProvisionalChunk(ctx, facts[start:end], tenantID, environmentID, now); err != nil {
				return err
			}
		}
		return nil
	}

	var err error
	if len(facts) <= maxRowsPerStmt {
		err = upsertAll(ctx)
	} else {
		err = r.client.WithTx(ctx, upsertAll)
	}
	if err != nil {
		SetSpanError(span, err)
		r.log.Error(ctx, "upsert provisional revenue facts failed",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"fact_count", len(facts),
		)
		return ierr.WithError(err).
			WithHint("Failed to upsert provisional revenue facts").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// upsertProvisionalChunk builds and executes the multi-row INSERT ... ON
// CONFLICT statement for one bounded chunk of facts. Callers guarantee
// len(facts) * len(revenueFactInsertColumns) stays under the bind-param cap.
func (r *revenueFactRepository) upsertProvisionalChunk(ctx context.Context, facts []*revenuefact.RevenueFact, tenantID, environmentID string, now time.Time) error {
	numCols := len(revenueFactInsertColumns)
	args := make([]interface{}, 0, len(facts)*numCols)
	valueGroups := make([]string, 0, len(facts))

	for i, f := range facts {
		computedAt := f.ComputedAt
		if computedAt.IsZero() {
			computedAt = now
		}
		version := f.Version
		if version == 0 {
			version = 1
		}

		placeholders := make([]string, numCols)
		base := i * numCols
		for j := 0; j < numCols; j++ {
			placeholders[j] = fmt.Sprintf("$%d", base+j+1)
		}
		valueGroups = append(valueGroups, "("+strings.Join(placeholders, ", ")+")")

		args = append(args,
			f.ID,
			tenantID,
			environmentID,
			f.CustomerID,
			f.SubscriptionID,
			f.SubLineItemID,
			f.PriceID,
			f.MeterID,
			enumStrPtr(f.AggregationType),
			string(f.RevenueSource),
			f.PeriodStart,
			f.PeriodEnd,
			f.Day,
			f.ServiceStart,
			f.ServiceEnd,
			enumStrPtr(f.RecognitionMethod),
			f.UsageAtListRate,
			f.TierDelta,
			f.EntitlementAmount,
			f.LineDiscount,
			f.InvoiceDiscount,
			f.NetAmount,
			f.BillableQty,
			f.EntitlementQty,
			string(f.DecompositionMode),
			f.Currency,
			string(types.FactProvisional),
			f.IsRevert,
			f.InvoiceID,
			f.InvoiceLineItemID,
			f.LockAdjustedDay,
			computedAt,
			version,
		)
	}

	setClauses := make([]string, 0, len(revenueFactMutableColumns)+1)
	for _, col := range revenueFactMutableColumns {
		setClauses = append(setClauses, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
	}
	setClauses = append(setClauses, "version = revenue_facts.version + 1")

	query := fmt.Sprintf(
		`INSERT INTO revenue_facts (%s) VALUES %s
		ON CONFLICT (tenant_id, environment_id, subscription_id, price_id, sub_line_item_id, day, revenue_source)
		WHERE status = 'PROVISIONAL'
		DO UPDATE SET %s`,
		revenueFactInsertColumnList,
		strings.Join(valueGroups, ", "),
		strings.Join(setClauses, ", "),
	)

	_, err := r.client.Writer(ctx).ExecContext(ctx, query, args...)
	return err
}

// FlipToFinal converts PROVISIONAL rows for a subscription/price/period to
// FINAL, stamping the given invoice line item. Returns the rows affected.
func (r *revenueFactRepository) FlipToFinal(ctx context.Context, subscriptionID, priceID string, periodStart, periodEnd time.Time, invoiceID, invoiceLineItemID string) (int, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "revenue_fact", "flip_to_final", map[string]interface{}{
		"tenant_id":       tenantID,
		"environment_id":  environmentID,
		"subscription_id": subscriptionID,
		"price_id":        priceID,
		"invoice_id":      invoiceID,
	})
	defer FinishSpan(span)

	// UpsertProvisional rejects empty/NULL price_id up front, so provisional
	// rows always carry a non-empty price_id; a plain equality match suffices.
	n, err := r.client.Writer(ctx).RevenueFact.Update().
		Where(
			entrevenuefact.TenantID(tenantID),
			entrevenuefact.EnvironmentID(environmentID),
			entrevenuefact.SubscriptionID(subscriptionID),
			entrevenuefact.PriceID(priceID),
			entrevenuefact.DayGTE(periodStart),
			entrevenuefact.DayLTE(periodEnd),
			entrevenuefact.StatusEQ(types.FactProvisional),
		).
		SetStatus(types.FactFinal).
		SetInvoiceID(invoiceID).
		SetInvoiceLineItemID(invoiceLineItemID).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		r.log.Error(ctx, "flip revenue facts to final failed",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"subscription_id", subscriptionID,
			"price_id", priceID,
			"invoice_id", invoiceID,
		)
		return 0, ierr.WithError(err).
			WithHint("Failed to flip revenue facts to final").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return n, nil
}

// ListBySubscriptionPeriod lists facts for a subscription within a period, filtered by status.
func (r *revenueFactRepository) ListBySubscriptionPeriod(ctx context.Context, subscriptionID string, periodStart, periodEnd time.Time, status types.FactStatus) ([]*revenuefact.RevenueFact, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "revenue_fact", "list_by_subscription_period", map[string]interface{}{
		"tenant_id":       tenantID,
		"environment_id":  environmentID,
		"subscription_id": subscriptionID,
		"status":          string(status),
	})
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).RevenueFact.Query().
		Where(
			entrevenuefact.TenantID(tenantID),
			entrevenuefact.EnvironmentID(environmentID),
			entrevenuefact.SubscriptionID(subscriptionID),
			entrevenuefact.DayGTE(periodStart),
			entrevenuefact.DayLTE(periodEnd),
			entrevenuefact.StatusEQ(status),
		).
		Order(ent.Asc(entrevenuefact.FieldDay)).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		r.log.Error(ctx, "list revenue facts by subscription period failed",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"subscription_id", subscriptionID,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to list revenue facts").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return revenuefact.FromEntList(rows), nil
}

// RevertByInvoice writes a negating twin for every FINAL fact of a voided
// invoice — FINAL rows are never edited. One transaction; idempotent.
func (r *revenueFactRepository) RevertByInvoice(ctx context.Context, invoiceID string) (int, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "revenue_fact", "revert_by_invoice", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
		"invoice_id":     invoiceID,
	})
	defer FinishSpan(span)

	var written int
	err := r.client.WithTx(ctx, func(ctx context.Context) error {
		rows, err := r.client.Writer(ctx).RevenueFact.Query().
			Where(
				entrevenuefact.TenantID(tenantID),
				entrevenuefact.EnvironmentID(environmentID),
				entrevenuefact.InvoiceID(invoiceID),
				entrevenuefact.StatusEQ(types.FactFinal),
			).
			All(ctx)
		if err != nil {
			return err
		}

		originals := make([]*ent.RevenueFact, 0, len(rows))
		for _, row := range rows {
			if row.IsRevert {
				// Already reverted (retried void hook) — nothing to do.
				return nil
			}
			originals = append(originals, row)
		}
		if len(originals) == 0 {
			return nil
		}

		now := time.Now().UTC()
		bulk := make([]*ent.RevenueFactCreate, 0, len(originals))
		for _, row := range originals {
			rev := revenuefact.NewRevert(revenuefact.FromEnt(row), now)
			bulk = append(bulk, r.client.Writer(ctx).RevenueFact.Create().
				SetID(rev.ID).
				SetTenantID(rev.TenantID).
				SetEnvironmentID(rev.EnvironmentID).
				SetCustomerID(rev.CustomerID).
				SetSubscriptionID(rev.SubscriptionID).
				SetNillableSubLineItemID(rev.SubLineItemID).
				SetNillablePriceID(rev.PriceID).
				SetNillableMeterID(rev.MeterID).
				SetNillableAggregationType(rev.AggregationType).
				SetRevenueSource(rev.RevenueSource).
				SetPeriodStart(rev.PeriodStart).
				SetPeriodEnd(rev.PeriodEnd).
				SetDay(rev.Day).
				SetNillableServiceStart(rev.ServiceStart).
				SetNillableServiceEnd(rev.ServiceEnd).
				SetNillableRecognitionMethod(rev.RecognitionMethod).
				SetUsageAtListRate(rev.UsageAtListRate).
				SetTierDelta(rev.TierDelta).
				SetEntitlementAmount(rev.EntitlementAmount).
				SetLineDiscount(rev.LineDiscount).
				SetInvoiceDiscount(rev.InvoiceDiscount).
				SetNetAmount(rev.NetAmount).
				SetBillableQty(rev.BillableQty).
				SetEntitlementQty(rev.EntitlementQty).
				SetDecompositionMode(rev.DecompositionMode).
				SetCurrency(rev.Currency).
				SetStatus(rev.Status).
				SetIsRevert(rev.IsRevert).
				SetNillableInvoiceID(rev.InvoiceID).
				SetNillableInvoiceLineItemID(rev.InvoiceLineItemID).
				SetNillableLockAdjustedDay(rev.LockAdjustedDay).
				SetComputedAt(rev.ComputedAt).
				SetVersion(rev.Version))
		}

		if _, err := r.client.Writer(ctx).RevenueFact.CreateBulk(bulk...).Save(ctx); err != nil {
			return err
		}
		written = len(bulk)
		return nil
	})
	if err != nil {
		SetSpanError(span, err)
		r.log.Error(ctx, "revert revenue facts by invoice failed",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"invoice_id", invoiceID,
		)
		return 0, ierr.WithError(err).
			WithHint("Failed to revert revenue facts for voided invoice").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return written, nil
}

// ListByInvoiceID lists every fact stamped with the invoice, reverts included.
func (r *revenueFactRepository) ListByInvoiceID(ctx context.Context, invoiceID string) ([]*revenuefact.RevenueFact, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "revenue_fact", "list_by_invoice", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
		"invoice_id":     invoiceID,
	})
	defer FinishSpan(span)

	rows, err := r.client.Reader(ctx).RevenueFact.Query().
		Where(
			entrevenuefact.TenantID(tenantID),
			entrevenuefact.EnvironmentID(environmentID),
			entrevenuefact.InvoiceID(invoiceID),
		).
		Order(ent.Asc(entrevenuefact.FieldDay)).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		r.log.Error(ctx, "list revenue facts by invoice failed",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
			"invoice_id", invoiceID,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to list revenue facts by invoice").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return revenuefact.FromEntList(rows), nil
}

// ListForExport pages facts recomputed after the watermark, ordered by
// (computed_at, id) for stable resumption.
func (r *revenueFactRepository) ListForExport(ctx context.Context, computedAfter time.Time, afterID string, limit int) ([]*revenuefact.RevenueFact, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "revenue_fact", "list_for_export", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
	})
	defer FinishSpan(span)

	q := r.client.Reader(ctx).RevenueFact.Query().
		Where(
			entrevenuefact.TenantID(tenantID),
			entrevenuefact.EnvironmentID(environmentID),
		)
	if afterID != "" {
		// Resume mid-instant: rows at the watermark instant with a larger id,
		// plus everything strictly after it.
		q = q.Where(entrevenuefact.Or(
			entrevenuefact.And(entrevenuefact.ComputedAt(computedAfter), entrevenuefact.IDGT(afterID)),
			entrevenuefact.ComputedAtGT(computedAfter),
		))
	} else if !computedAfter.IsZero() {
		q = q.Where(entrevenuefact.ComputedAtGT(computedAfter))
	}

	rows, err := q.
		Order(ent.Asc(entrevenuefact.FieldComputedAt), ent.Asc(entrevenuefact.FieldID)).
		Limit(limit).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		r.log.Error(ctx, "list revenue facts for export failed",
			"error", err,
			"tenant_id", tenantID,
			"environment_id", environmentID,
		)
		return nil, ierr.WithError(err).
			WithHint("Failed to list revenue facts for export").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return revenuefact.FromEntList(rows), nil
}
