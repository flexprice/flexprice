package ent

import (
	"context"
	"time"

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

func (r *meterUsageCheckpointRepository) GetLatestAtOrBefore(
	ctx context.Context,
	key domainCheckpoint.Key,
	at time.Time,
) (*domainCheckpoint.MeterUsageCheckpoint, error) {
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "get_latest", map[string]interface{}{
		"customer_id":      key.CustomerID,
		"meter_id":         key.MeterID,
		"aggregation_type": key.AggregationType,
	})
	defer FinishSpan(span)

	row, err := r.client.Reader(ctx).MeterUsageCheckpoint.Query().
		Where(
			meterusagecheckpoint.TenantID(types.GetTenantID(ctx)),
			meterusagecheckpoint.EnvironmentID(types.GetEnvironmentID(ctx)),
			meterusagecheckpoint.CustomerID(key.CustomerID),
			meterusagecheckpoint.MeterID(key.MeterID),
			meterusagecheckpoint.AggregationType(key.AggregationType),
			meterusagecheckpoint.AggregatedUntilLTE(at),
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
	span := StartRepositorySpan(ctx, "meter_usage_checkpoint", "upsert", map[string]interface{}{
		"customer_id":      c.CustomerID,
		"meter_id":         c.MeterID,
		"aggregated_until": c.AggregatedUntil,
	})
	defer FinishSpan(span)

	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	saved, err := r.updateExisting(ctx, c)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	if saved != nil {
		return saved, nil
	}

	created, err := r.client.Writer(ctx).MeterUsageCheckpoint.Create().
		SetID(c.ID).
		SetCustomerID(c.CustomerID).
		SetMeterID(c.MeterID).
		SetAggregationType(c.AggregationType).
		SetTotal(c.Total).
		SetAggregatedUntil(c.AggregatedUntil).
		SetNillableInvoiceID(c.InvoiceID).
		SetTenantID(c.TenantID).
		SetEnvironmentID(c.EnvironmentID).
		SetStatus(string(c.Status)).
		SetCreatedAt(c.CreatedAt).
		SetUpdatedAt(c.UpdatedAt).
		SetCreatedBy(c.CreatedBy).
		SetUpdatedBy(c.UpdatedBy).
		Save(ctx)
	if err == nil {
		return domainCheckpoint.FromEnt(created), nil
	}

	// A concurrent writer created the same checkpoint first; take its row.
	if ent.IsConstraintError(err) {
		if saved, retryErr := r.updateExisting(ctx, c); retryErr == nil && saved != nil {
			return saved, nil
		}
	}
	SetSpanError(span, err)
	return nil, ierr.WithError(err).
		WithHint("Failed to save meter usage checkpoint").
		WithReportableDetails(map[string]interface{}{
			"customer_id": c.CustomerID,
			"meter_id":    c.MeterID,
		}).
		Mark(ierr.ErrDatabase)
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
		SetAggregationType(c.AggregationType).
		SetTotal(c.Total).
		SetNillableInvoiceID(c.InvoiceID).
		SetStatus(string(types.StatusPublished)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to update meter usage checkpoint").
			Mark(ierr.ErrDatabase)
	}
	return domainCheckpoint.FromEnt(updated), nil
}
