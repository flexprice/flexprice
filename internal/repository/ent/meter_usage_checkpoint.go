package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/meterusagecheckpoint"
	"github.com/flexprice/flexprice/ent/meterusageperiodcheckpoint"
	domainCheckpoint "github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
	"github.com/samber/lo"
)

type meterUsageCheckpointRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewMeterUsageCheckpointRepository(client postgres.IClient, log *logger.Logger) domainCheckpoint.Repository {
	return &meterUsageCheckpointRepository{client: client, log: log}
}

func (r *meterUsageCheckpointRepository) GetLatestBefore(
	ctx context.Context,
	params domainCheckpoint.GetLatestBeforeParams,
) (*domainCheckpoint.MeterUsageCheckpoint, error) {
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "get_latest_before", map[string]interface{}{
		"customer_id":      params.CustomerID,
		"meter_id":         params.MeterID,
		"aggregation_type": params.AggregationType,
	})
	defer FinishSpan(span)

	row, err := r.client.Writer(ctx).MeterUsageCheckpoint.Query().
		Where(
			meterusagecheckpoint.TenantID(types.GetTenantID(ctx)),
			meterusagecheckpoint.EnvironmentID(types.GetEnvironmentID(ctx)),
			meterusagecheckpoint.CustomerID(params.CustomerID),
			meterusagecheckpoint.MeterID(params.MeterID),
			meterusagecheckpoint.AggregationType(params.AggregationType),
			meterusagecheckpoint.AggregatedUntilLT(params.Before),
			meterusagecheckpoint.PeriodStartIsNil(),
			meterusagecheckpoint.Status(string(types.StatusPublished)),
		).
		Order(ent.Desc(meterusagecheckpoint.FieldAggregatedUntil)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to look up meter usage checkpoint").
			Mark(ierr.ErrDatabase)
	}
	return domainCheckpoint.FromEnt(row), nil
}

func (r *meterUsageCheckpointRepository) Upsert(
	ctx context.Context,
	c *domainCheckpoint.MeterUsageCheckpoint,
) (*domainCheckpoint.MeterUsageCheckpoint, error) {
	if c == nil || c.ID == "" || c.CustomerID == "" || c.MeterID == "" || c.AggregatedUntil.IsZero() || !c.AggregationType.Validate() {
		return nil, ierr.NewError("invalid meter usage checkpoint").Mark(ierr.ErrValidation)
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || c.TenantID != tenantID || (c.EnvironmentID != "" && c.EnvironmentID != environmentID) {
		return nil, ierr.NewError("checkpoint scope does not match request context").Mark(ierr.ErrValidation)
	}
	checkpoint := *c
	checkpoint.EnvironmentID = environmentID
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "upsert", map[string]interface{}{
		"customer_id": c.CustomerID, "meter_id": c.MeterID, "aggregated_until": c.AggregatedUntil,
	})
	defer FinishSpan(span)
	var saved *domainCheckpoint.MeterUsageCheckpoint
	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		key := fmt.Sprintf("meter_usage_checkpoint:%s:%s:%s:%s", tenantID, environmentID, c.CustomerID, c.MeterID)
		if err := r.client.LockWithWait(txCtx, postgres.LockRequest{Key: key}); err != nil {
			return err
		}
		existing, err := r.updateExisting(txCtx, &checkpoint)
		if err != nil {
			return err
		}
		if existing != nil {
			saved = existing
			return nil
		}
		created, err := r.client.Writer(txCtx).MeterUsageCheckpoint.Create().
			SetID(checkpoint.ID).
			SetCustomerID(checkpoint.CustomerID).
			SetMeterID(checkpoint.MeterID).
			SetAggregationType(checkpoint.AggregationType).
			SetTotal(checkpoint.Total).
			SetAggregatedUntil(checkpoint.AggregatedUntil).
			SetNillableInvoiceID(checkpoint.InvoiceID).
			SetTenantID(tenantID).
			SetEnvironmentID(environmentID).
			SetStatus(string(types.StatusPublished)).
			SetCreatedAt(checkpoint.CreatedAt).
			SetUpdatedAt(checkpoint.UpdatedAt).
			SetCreatedBy(checkpoint.CreatedBy).
			SetUpdatedBy(checkpoint.UpdatedBy).
			Save(txCtx)
		if err != nil {
			return err
		}
		saved = domainCheckpoint.FromEnt(created)
		return nil
	})
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to save meter usage checkpoint").Mark(ierr.ErrDatabase)
	}
	return saved, nil
}

// updateExisting sets the total on the checkpoint with the same key, returning nil
// when there is none yet.
func (r *meterUsageCheckpointRepository) updateExisting(
	ctx context.Context,
	c *domainCheckpoint.MeterUsageCheckpoint,
) (*domainCheckpoint.MeterUsageCheckpoint, error) {
	client := r.client.Writer(ctx)
	existing, err := client.MeterUsageCheckpoint.Query().
		Where(
			meterusagecheckpoint.TenantID(types.GetTenantID(ctx)),
			meterusagecheckpoint.EnvironmentID(c.EnvironmentID),
			meterusagecheckpoint.CustomerID(c.CustomerID),
			meterusagecheckpoint.MeterID(c.MeterID),
			meterusagecheckpoint.AggregatedUntil(c.AggregatedUntil),
			meterusagecheckpoint.PeriodStartIsNil(),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to look up meter usage checkpoint").
			Mark(ierr.ErrDatabase)
	}

	updated, err := client.MeterUsageCheckpoint.UpdateOneID(existing.ID).
		Where(meterusagecheckpoint.TenantID(types.GetTenantID(ctx)), meterusagecheckpoint.EnvironmentID(types.GetEnvironmentID(ctx))).
		SetAggregationType(c.AggregationType).
		SetTotal(c.Total).
		SetNillableInvoiceID(c.InvoiceID).
		SetStatus(string(types.StatusPublished)).
		SetUpdatedAt(clock.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update meter usage checkpoint").
			Mark(ierr.ErrDatabase)
	}
	return domainCheckpoint.FromEnt(updated), nil
}

func (r *meterUsageCheckpointRepository) ListPeriodTotals(
	ctx context.Context,
	customerID string,
	periodStart time.Time,
) ([]*domainCheckpoint.MeterUsageCheckpoint, error) {
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "list_period_totals", map[string]interface{}{
		"customer_id": customerID, "period_start": periodStart,
	})
	defer FinishSpan(span)

	rows, err := r.client.Writer(ctx).MeterUsageCheckpoint.Query().
		Where(
			meterusagecheckpoint.TenantID(types.GetTenantID(ctx)),
			meterusagecheckpoint.EnvironmentID(types.GetEnvironmentID(ctx)),
			meterusagecheckpoint.CustomerID(customerID),
			meterusagecheckpoint.PeriodStart(periodStart),
			meterusagecheckpoint.Status(string(types.StatusPublished)),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to list per-period meter usage totals").Mark(ierr.ErrDatabase)
	}
	return lo.Map(rows, func(row *ent.MeterUsageCheckpoint, _ int) *domainCheckpoint.MeterUsageCheckpoint {
		return domainCheckpoint.FromEnt(row)
	}), nil
}

func (r *meterUsageCheckpointRepository) ListPeriodCheckpoints(
	ctx context.Context,
	customerIDs []string,
) ([]*domainCheckpoint.PeriodCheckpoint, error) {
	if len(customerIDs) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "list_period_checkpoints", map[string]interface{}{
		"customers": len(customerIDs),
	})
	defer FinishSpan(span)

	rows, err := r.client.Writer(ctx).MeterUsagePeriodCheckpoint.Query().
		Where(
			meterusageperiodcheckpoint.TenantID(types.GetTenantID(ctx)),
			meterusageperiodcheckpoint.EnvironmentID(types.GetEnvironmentID(ctx)),
			meterusageperiodcheckpoint.CustomerIDIn(customerIDs...),
			meterusageperiodcheckpoint.Status(string(types.StatusPublished)),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to list period checkpoints").Mark(ierr.ErrDatabase)
	}
	return lo.Map(rows, func(row *ent.MeterUsagePeriodCheckpoint, _ int) *domainCheckpoint.PeriodCheckpoint {
		return &domainCheckpoint.PeriodCheckpoint{CustomerID: row.CustomerID, PeriodStart: row.PeriodStart, AggregatedUntil: row.AggregatedUntil}
	}), nil
}

// Creates a period's checkpoint at its From when it has none, so every advance below is a guarded update.
const insertMissingPeriodCheckpointsSQL = `
INSERT INTO meter_usage_period_checkpoints
	(id, tenant_id, environment_id, status, created_at, updated_at, created_by, updated_by, customer_id, period_start, aggregated_until)
SELECT v.id, $1, $2, $3, $4, $4, NULLIF($5, ''), NULLIF($5, ''), v.customer_id, v.period_start, v.from_ts
FROM unnest($6::text[], $7::text[], $8::timestamptz[], $9::timestamptz[]) AS v(id, customer_id, period_start, from_ts)
ON CONFLICT (tenant_id, environment_id, customer_id, period_start) DO NOTHING`

// Moves each checkpoint still at its From to its To, returning the periods it moved.
const advancePeriodCheckpointsSQL = `
UPDATE meter_usage_period_checkpoints c
SET aggregated_until = v.to_ts, updated_at = $4, updated_by = NULLIF($5, '')
FROM unnest($6::text[], $7::timestamptz[], $8::timestamptz[], $9::timestamptz[]) AS v(customer_id, period_start, from_ts, to_ts)
WHERE c.tenant_id = $1 AND c.environment_id = $2 AND c.status = $3
	AND c.customer_id = v.customer_id AND c.period_start = v.period_start AND c.aggregated_until = v.from_ts
RETURNING c.customer_id, c.period_start`

// Adds each meter's window quantity to its period total: MAX keeps the higher value, SUM and COUNT add.
const addPeriodTotalsSQL = `
INSERT INTO meter_usage_checkpoints
	(id, tenant_id, environment_id, status, created_at, updated_at, created_by, updated_by,
	 customer_id, meter_id, aggregation_type, total, aggregated_until, period_start)
SELECT v.id, $1, $2, $3, $4, $4, NULLIF($5, ''), NULLIF($5, ''),
	v.customer_id, v.meter_id, v.aggregation_type, v.quantity, v.to_ts, v.period_start
FROM unnest($6::text[], $7::text[], $8::text[], $9::text[], $10::numeric[], $11::timestamptz[], $12::timestamptz[])
	AS v(id, customer_id, meter_id, aggregation_type, quantity, period_start, to_ts)
ON CONFLICT (tenant_id, environment_id, customer_id, meter_id, period_start) WHERE period_start IS NOT NULL
DO UPDATE SET
	total = CASE WHEN meter_usage_checkpoints.aggregation_type = $13
		THEN GREATEST(meter_usage_checkpoints.total, EXCLUDED.total)
		ELSE meter_usage_checkpoints.total + EXCLUDED.total END,
	aggregated_until = EXCLUDED.aggregated_until,
	updated_at = EXCLUDED.updated_at,
	updated_by = EXCLUDED.updated_by`

func (r *meterUsageCheckpointRepository) AdvancePeriods(
	ctx context.Context,
	advances []*domainCheckpoint.PeriodAdvance,
) (int, error) {
	if len(advances) == 0 {
		return 0, nil
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return 0, ierr.NewError("tenant and environment are required").Mark(ierr.ErrValidation)
	}
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "advance_periods", map[string]interface{}{
		"periods": len(advances),
	})
	defer FinishSpan(span)

	now, userID, published := clock.Now(), types.GetUserID(ctx), string(types.StatusPublished)
	var ids, customers, periodStarts, froms, tos []string
	for _, a := range advances {
		ids = append(ids, types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_PERIOD_CHECKPOINT))
		customers = append(customers, a.CustomerID)
		periodStarts = append(periodStarts, formatTimestamp(a.PeriodStart))
		froms = append(froms, formatTimestamp(a.From))
		tos = append(tos, formatTimestamp(a.To))
	}

	advanced := 0
	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		db := r.client.Writer(txCtx)
		if _, err := db.ExecContext(txCtx, insertMissingPeriodCheckpointsSQL, tenantID, environmentID, published, now, userID,
			pq.Array(ids), pq.Array(customers), pq.Array(periodStarts), pq.Array(froms)); err != nil {
			return err
		}
		rows, err := db.QueryContext(txCtx, advancePeriodCheckpointsSQL, tenantID, environmentID, published, now, userID,
			pq.Array(customers), pq.Array(periodStarts), pq.Array(froms), pq.Array(tos))
		if err != nil {
			return err
		}
		moved := make(map[string]bool, len(advances))
		for rows.Next() {
			var customerID string
			var periodStart time.Time
			if err := rows.Scan(&customerID, &periodStart); err != nil {
				rows.Close()
				return err
			}
			moved[customerID+"|"+formatTimestamp(periodStart)] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		advanced = len(moved)

		var totalIDs, totalCustomers, meters, aggregations, quantities, totalPeriods, totalTos []string
		for _, a := range advances {
			if !moved[a.CustomerID+"|"+formatTimestamp(a.PeriodStart)] {
				continue
			}
			for _, t := range a.Totals {
				totalIDs = append(totalIDs, types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT))
				totalCustomers = append(totalCustomers, a.CustomerID)
				meters = append(meters, t.MeterID)
				aggregations = append(aggregations, string(t.AggregationType))
				quantities = append(quantities, t.Quantity.String())
				totalPeriods = append(totalPeriods, formatTimestamp(a.PeriodStart))
				totalTos = append(totalTos, formatTimestamp(a.To))
			}
		}
		if len(totalIDs) == 0 {
			return nil
		}
		_, err = db.ExecContext(txCtx, addPeriodTotalsSQL, tenantID, environmentID, published, now, userID,
			pq.Array(totalIDs), pq.Array(totalCustomers), pq.Array(meters), pq.Array(aggregations),
			pq.Array(quantities), pq.Array(totalPeriods), pq.Array(totalTos), string(types.AggregationMax))
		return err
	})
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).WithHint("Failed to advance period checkpoints").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return advanced, nil
}

func formatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}
