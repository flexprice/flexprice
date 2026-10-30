package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/meterusagecheckpoint"
	domainCheckpoint "github.com/flexprice/flexprice/internal/domain/meterusagecheckpoint"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
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
		SetUpdatedAt(types.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update meter usage checkpoint").
			Mark(ierr.ErrDatabase)
	}
	return domainCheckpoint.FromEnt(updated), nil
}
