package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/clock"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/meterusagecheckpoint"
	"github.com/flexprice/flexprice/ent/meterusagecheckpointwatermark"
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

func (r *meterUsageCheckpointRepository) ListMeterUsageCheckpointWatermarks(
	ctx context.Context,
	customerIDs []string,
) ([]*domainCheckpoint.MeterUsageCheckpointWatermark, error) {
	if len(customerIDs) == 0 {
		return nil, nil
	}
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "list_meter_usage_checkpoint_watermarks", map[string]interface{}{
		"customers": len(customerIDs),
	})
	defer FinishSpan(span)

	rows, err := r.client.Writer(ctx).MeterUsageCheckpointWatermark.Query().
		Where(
			meterusagecheckpointwatermark.TenantID(types.GetTenantID(ctx)),
			meterusagecheckpointwatermark.EnvironmentID(types.GetEnvironmentID(ctx)),
			meterusagecheckpointwatermark.CustomerIDIn(customerIDs...),
			meterusagecheckpointwatermark.Status(string(types.StatusPublished)),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to list meter usage checkpoint watermarks").Mark(ierr.ErrDatabase)
	}
	return lo.Map(rows, func(row *ent.MeterUsageCheckpointWatermark, _ int) *domainCheckpoint.MeterUsageCheckpointWatermark {
		return &domainCheckpoint.MeterUsageCheckpointWatermark{CustomerID: row.CustomerID, AggregatedUntil: row.AggregatedUntil}
	}), nil
}

// Upserts each customer's watermark to its To. An existing watermark moves only while it is still at the
// From the run read (NULL for a customer that had none), so a retried run never adds usage twice.
const upsertWatermarksSQL = `
WITH v AS (
	SELECT * FROM unnest($6::text[], $7::text[], $8::text[], $9::timestamptz[]) AS v(id, customer_id, from_ts, to_ts)
)
INSERT INTO meter_usage_checkpoint_watermarks AS w
	(id, tenant_id, environment_id, status, created_at, updated_at, created_by, updated_by, customer_id, aggregated_until)
SELECT v.id, $1, $2, $3, $4, $4, NULLIF($5, ''), NULLIF($5, ''), v.customer_id, v.to_ts FROM v
ON CONFLICT (tenant_id, environment_id, customer_id) DO UPDATE
SET aggregated_until = EXCLUDED.aggregated_until, updated_at = EXCLUDED.updated_at, updated_by = EXCLUDED.updated_by
WHERE w.aggregated_until = (SELECT NULLIF(v.from_ts, '')::timestamptz FROM v WHERE v.customer_id = EXCLUDED.customer_id)
RETURNING w.customer_id`

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

func (r *meterUsageCheckpointRepository) UpdateCustomerCheckpoints(
	ctx context.Context,
	updates []*domainCheckpoint.CustomerCheckpointUpdate,
) (int, error) {
	if len(updates) == 0 {
		return 0, nil
	}
	tenantID, environmentID := types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
	if tenantID == "" || environmentID == "" {
		return 0, ierr.NewError("tenant and environment are required").Mark(ierr.ErrValidation)
	}
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "update_customer_checkpoints", map[string]interface{}{
		"customers": len(updates),
	})
	defer FinishSpan(span)

	now, userID, published := clock.Now(), types.GetUserID(ctx), string(types.StatusPublished)
	var ids, customers, froms, tos []string
	for _, u := range updates {
		ids = append(ids, types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT_WATERMARK))
		customers = append(customers, u.CustomerID)
		froms = append(froms, formatOptionalTimestamp(u.From))
		tos = append(tos, formatTimestamp(u.To))
	}

	applied := make(map[string]bool, len(updates))
	err := r.client.WithTx(ctx, func(txCtx context.Context) error {
		db := r.client.Writer(txCtx)
		rows, err := db.QueryContext(txCtx, upsertWatermarksSQL, tenantID, environmentID, published, now, userID,
			pq.Array(ids), pq.Array(customers), pq.Array(froms), pq.Array(tos))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var customerID string
			if err := rows.Scan(&customerID); err != nil {
				return err
			}
			applied[customerID] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}

		var totalIDs, totalCustomers, meters, aggregations, quantities, periodStarts, totalTos []string
		for _, u := range updates {
			if !applied[u.CustomerID] {
				continue
			}
			for _, t := range u.Totals {
				totalIDs = append(totalIDs, types.GenerateUUIDWithPrefix(types.UUID_PREFIX_METER_USAGE_CHECKPOINT))
				totalCustomers = append(totalCustomers, u.CustomerID)
				meters = append(meters, t.MeterID)
				aggregations = append(aggregations, string(t.AggregationType))
				quantities = append(quantities, t.Quantity.String())
				periodStarts = append(periodStarts, formatTimestamp(t.PeriodStart))
				totalTos = append(totalTos, formatTimestamp(u.To))
			}
		}
		if len(totalIDs) == 0 {
			return nil
		}
		_, err = db.ExecContext(txCtx, addPeriodTotalsSQL, tenantID, environmentID, published, now, userID,
			pq.Array(totalIDs), pq.Array(totalCustomers), pq.Array(meters), pq.Array(aggregations),
			pq.Array(quantities), pq.Array(periodStarts), pq.Array(totalTos), string(types.AggregationMax))
		return err
	})
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).WithHint("Failed to update customer usage checkpoints").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return len(applied), nil
}

func formatTimestamp(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// formatOptionalTimestamp is formatTimestamp, or "" (NULL in the SQL above) for the zero time.
func formatOptionalTimestamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return formatTimestamp(t)
}
