package ent

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	entSSB "github.com/flexprice/flexprice/ent/stripesyncbatch"
	"github.com/flexprice/flexprice/internal/cache"
	domainIntegration "github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type stripeSyncBatchRepository struct {
	client postgres.IClient
	log    *logger.Logger
	cache  cache.Cache
}

func NewStripeSyncBatchRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainIntegration.StripeSyncBatchRepository {
	return &stripeSyncBatchRepository{
		client: client,
		log:    log,
		cache:  cache,
	}
}

// Basic CRUD operations - placeholder implementations until Ent schemas are generated
func (r *stripeSyncBatchRepository) Create(ctx context.Context, batch *domainIntegration.StripeSyncBatch) error {
	span := StartRepositorySpan(ctx, "stripe_sync_batch", "create", map[string]interface{}{"batch_id": batch.ID})
	defer FinishSpan(span)

	if err := batch.Validate(); err != nil {
		return err
	}

	c := r.client.Querier(ctx)
	_, err := c.StripeSyncBatch.Create().
		SetID(batch.ID).
		SetTenantID(batch.TenantID).
		SetStatus(string(batch.Status)).
		SetCreatedAt(batch.CreatedAt).
		SetUpdatedAt(batch.UpdatedAt).
		SetCreatedBy(batch.CreatedBy).
		SetUpdatedBy(batch.UpdatedBy).
		SetEnvironmentID(batch.EnvironmentID).
		SetEntityID(batch.EntityID).
		SetEntityType(string(batch.EntityType)).
		SetMeterID(batch.MeterID).
		SetEventType(batch.EventType).
		SetAggregatedQuantity(batch.AggregatedQuantity).
		SetEventCount(batch.EventCount).
		SetStripeEventID(batch.StripeEventID).
		SetSyncStatus(string(batch.SyncStatus)).
		SetRetryCount(batch.RetryCount).
		SetErrorMessage(batch.ErrorMessage).
		SetWindowStart(batch.WindowStart).
		SetWindowEnd(batch.WindowEnd).
		SetNillableSyncedAt(batch.SyncedAt).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return nil
}

func (r *stripeSyncBatchRepository) Get(ctx context.Context, id string) (*domainIntegration.StripeSyncBatch, error) {
	c := r.client.Querier(ctx)
	sb, err := c.StripeSyncBatch.Query().Where(
		entSSB.ID(id), entSSB.TenantID(types.GetTenantID(ctx)), entSSB.EnvironmentID(types.GetEnvironmentID(ctx))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return toDomainSB(sb), nil
}

func (r *stripeSyncBatchRepository) List(ctx context.Context, filter *domainIntegration.StripeSyncBatchFilter) ([]*domainIntegration.StripeSyncBatch, error) {
	if filter == nil {
		filter = &domainIntegration.StripeSyncBatchFilter{}
	}
	q := r.buildFilterQuery(ctx, filter)
	items, err := q.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	res := make([]*domainIntegration.StripeSyncBatch, 0, len(items))
	for _, sb := range items {
		res = append(res, toDomainSB(sb))
	}
	return res, nil
}

func (r *stripeSyncBatchRepository) Count(ctx context.Context, filter *domainIntegration.StripeSyncBatchFilter) (int, error) {
	if filter == nil {
		filter = &domainIntegration.StripeSyncBatchFilter{}
	}
	q := r.buildFilterQuery(ctx, filter)
	n, err := q.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return n, nil
}

func (r *stripeSyncBatchRepository) Update(ctx context.Context, batch *domainIntegration.StripeSyncBatch) error {
	c := r.client.Querier(ctx)
	_, err := c.StripeSyncBatch.Update().Where(
		entSSB.ID(batch.ID), entSSB.TenantID(types.GetTenantID(ctx)), entSSB.EnvironmentID(types.GetEnvironmentID(ctx))).
		SetAggregatedQuantity(batch.AggregatedQuantity).
		SetEventCount(batch.EventCount).
		SetStripeEventID(batch.StripeEventID).
		SetSyncStatus(string(batch.SyncStatus)).
		SetRetryCount(batch.RetryCount).
		SetErrorMessage(batch.ErrorMessage).
		SetNillableSyncedAt(batch.SyncedAt).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return nil
}

func (r *stripeSyncBatchRepository) Delete(ctx context.Context, batch *domainIntegration.StripeSyncBatch) error {
	c := r.client.Querier(ctx)
	_, err := c.StripeSyncBatch.Update().Where(
		entSSB.ID(batch.ID), entSSB.TenantID(types.GetTenantID(ctx)), entSSB.EnvironmentID(types.GetEnvironmentID(ctx))).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return nil
}

// Sync-specific methods
func (r *stripeSyncBatchRepository) GetByTimeWindow(ctx context.Context, entityID string, entityType domainIntegration.EntityType, meterID string, windowStart, windowEnd time.Time) (*domainIntegration.StripeSyncBatch, error) {
	c := r.client.Querier(ctx)
	sb, err := c.StripeSyncBatch.Query().Where(
		entSSB.TenantID(types.GetTenantID(ctx)),
		entSSB.EnvironmentID(types.GetEnvironmentID(ctx)),
		entSSB.EntityID(entityID),
		entSSB.EntityType(string(entityType)),
		entSSB.MeterID(meterID),
		entSSB.WindowStartEQ(windowStart),
		entSSB.WindowEndEQ(windowEnd),
		entSSB.StatusEQ(string(types.StatusPublished))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return toDomainSB(sb), nil
}

func (r *stripeSyncBatchRepository) ListByStatus(ctx context.Context, status domainIntegration.SyncStatus, filter *domainIntegration.StripeSyncBatchFilter) ([]*domainIntegration.StripeSyncBatch, error) {
	if filter == nil {
		filter = &domainIntegration.StripeSyncBatchFilter{}
	}
	filter.SyncStatuses = []domainIntegration.SyncStatus{status}
	return r.List(ctx, filter)
}

func (r *stripeSyncBatchRepository) ListFailedBatches(ctx context.Context, filter *domainIntegration.StripeSyncBatchFilter) ([]*domainIntegration.StripeSyncBatch, error) {
	return r.ListByStatus(ctx, domainIntegration.SyncStatusFailed, filter)
}

func (r *stripeSyncBatchRepository) ListRetryableBatches(ctx context.Context, filter *domainIntegration.StripeSyncBatchFilter) ([]*domainIntegration.StripeSyncBatch, error) {
	if filter == nil {
		filter = &domainIntegration.StripeSyncBatchFilter{}
	}
	filter.SyncStatuses = []domainIntegration.SyncStatus{domainIntegration.SyncStatusFailed}
	q := r.buildFilterQuery(ctx, filter).Where(entSSB.RetryCountLT(domainIntegration.MaxRetryCount))
	list, err := q.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	res := make([]*domainIntegration.StripeSyncBatch, 0, len(list))
	for _, sb := range list {
		res = append(res, toDomainSB(sb))
	}
	return res, nil
}

func (r *stripeSyncBatchRepository) UpdateStatus(ctx context.Context, id string, status domainIntegration.SyncStatus, errMsg string) error {
	c := r.client.Querier(ctx)
	_, err := c.StripeSyncBatch.Update().Where(
		entSSB.ID(id), entSSB.TenantID(types.GetTenantID(ctx)), entSSB.EnvironmentID(types.GetEnvironmentID(ctx))).
		SetSyncStatus(string(status)).
		SetErrorMessage(errMsg).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return nil
}

func (r *stripeSyncBatchRepository) CleanupOldBatches(ctx context.Context, olderThan time.Time) (int, error) {
	c := r.client.Querier(ctx)
	// Archive (soft-delete) batches whose window_end is older than given time and already completed/failed
	res, err := c.StripeSyncBatch.Update().
		Where(
			entSSB.TenantID(types.GetTenantID(ctx)),
			entSSB.EnvironmentID(types.GetEnvironmentID(ctx)),
			entSSB.WindowEndLT(olderThan),
			entSSB.StatusEQ(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		return 0, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return res, nil
}

func (r *stripeSyncBatchRepository) BulkCreate(ctx context.Context, batches []*domainIntegration.StripeSyncBatch) error {
	if len(batches) == 0 {
		return nil
	}
	c := r.client.Querier(ctx)
	builders := make([]*ent.StripeSyncBatchCreate, 0, len(batches))
	for _, b := range batches {
		builders = append(builders, c.StripeSyncBatch.Create().
			SetID(b.ID).
			SetTenantID(b.TenantID).
			SetStatus(string(b.Status)).
			SetCreatedAt(b.CreatedAt).
			SetUpdatedAt(b.UpdatedAt).
			SetCreatedBy(b.CreatedBy).
			SetUpdatedBy(b.UpdatedBy).
			SetEnvironmentID(b.EnvironmentID).
			SetEntityID(b.EntityID).
			SetEntityType(string(b.EntityType)).
			SetMeterID(b.MeterID).
			SetEventType(b.EventType).
			SetAggregatedQuantity(b.AggregatedQuantity).
			SetEventCount(b.EventCount).
			SetStripeEventID(b.StripeEventID).
			SetSyncStatus(string(b.SyncStatus)).
			SetRetryCount(b.RetryCount).
			SetErrorMessage(b.ErrorMessage).
			SetWindowStart(b.WindowStart).
			SetWindowEnd(b.WindowEnd).
			SetNillableSyncedAt(b.SyncedAt))
	}
	if err := c.StripeSyncBatch.CreateBulk(builders...).Exec(ctx); err != nil {
		// Ignore duplicate key errors to make operation idempotent
		if strings.Contains(err.Error(), "duplicate key value") {
			r.log.Warnw("duplicate stripe_sync_batch record ignored", "error", err)
			return nil
		}
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return nil
}

// helper query builder
func (r *stripeSyncBatchRepository) buildFilterQuery(ctx context.Context, f *domainIntegration.StripeSyncBatchFilter) *ent.StripeSyncBatchQuery {
	c := r.client.Querier(ctx)
	q := c.StripeSyncBatch.Query().Where(
		entSSB.TenantID(types.GetTenantID(ctx)), entSSB.EnvironmentID(types.GetEnvironmentID(ctx)))
	if len(f.EntityIDs) > 0 {
		q = q.Where(entSSB.EntityIDIn(f.EntityIDs...))
	}
	if len(f.EntityTypes) > 0 {
		et := make([]string, len(f.EntityTypes))
		for i, v := range f.EntityTypes {
			et[i] = string(v)
		}
		q = q.Where(entSSB.EntityTypeIn(et...))
	}
	if len(f.MeterIDs) > 0 {
		q = q.Where(entSSB.MeterIDIn(f.MeterIDs...))
	}
	if len(f.SyncStatuses) > 0 {
		ss := make([]string, len(f.SyncStatuses))
		for i, v := range f.SyncStatuses {
			ss[i] = string(v)
		}
		q = q.Where(entSSB.SyncStatusIn(ss...))
	}
	if f.WindowStartAfter != nil {
		q = q.Where(entSSB.WindowStartGT(*f.WindowStartAfter))
	}
	if f.WindowStartBefore != nil {
		q = q.Where(entSSB.WindowStartLT(*f.WindowStartBefore))
	}
	if f.WindowEndAfter != nil {
		q = q.Where(entSSB.WindowEndGT(*f.WindowEndAfter))
	}
	if f.WindowEndBefore != nil {
		q = q.Where(entSSB.WindowEndLT(*f.WindowEndBefore))
	}
	if f.SyncedAfter != nil {
		q = q.Where(entSSB.SyncedAtGT(*f.SyncedAfter))
	}
	if f.SyncedBefore != nil {
		q = q.Where(entSSB.SyncedAtLT(*f.SyncedBefore))
	}
	if !f.IsUnlimited() {
		q = q.Limit(f.GetLimit()).Offset(f.GetOffset())
	}
	return q
}

// toDomainSB converts ent model to domain
func toDomainSB(sb *ent.StripeSyncBatch) *domainIntegration.StripeSyncBatch {
	if sb == nil {
		return nil
	}
	return &domainIntegration.StripeSyncBatch{
		ID:                 sb.ID,
		EntityID:           sb.EntityID,
		EntityType:         domainIntegration.EntityType(sb.EntityType),
		MeterID:            sb.MeterID,
		EventType:          sb.EventType,
		AggregatedQuantity: sb.AggregatedQuantity,
		EventCount:         sb.EventCount,
		StripeEventID:      sb.StripeEventID,
		SyncStatus:         domainIntegration.SyncStatus(sb.SyncStatus),
		RetryCount:         sb.RetryCount,
		ErrorMessage:       sb.ErrorMessage,
		WindowStart:        sb.WindowStart,
		WindowEnd:          sb.WindowEnd,
		SyncedAt:           sb.SyncedAt,
		EnvironmentID:      sb.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  sb.TenantID,
			Status:    types.Status(sb.Status),
			CreatedAt: sb.CreatedAt,
			UpdatedAt: sb.UpdatedAt,
			CreatedBy: sb.CreatedBy,
			UpdatedBy: sb.UpdatedBy,
		},
	}
}
