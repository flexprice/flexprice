package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entMPM "github.com/flexprice/flexprice/ent/meterprovidermapping"
	"github.com/flexprice/flexprice/internal/cache"
	domainIntegration "github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type meterProviderMappingRepository struct {
	client postgres.IClient
	log    *logger.Logger
	cache  cache.Cache
}

func NewMeterProviderMappingRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainIntegration.MeterProviderMappingRepository {
	return &meterProviderMappingRepository{
		client: client,
		log:    log,
		cache:  cache,
	}
}

// Basic CRUD operations - placeholder implementations until Ent schemas are generated
func (r *meterProviderMappingRepository) Create(ctx context.Context, mapping *domainIntegration.MeterProviderMapping) error {
	span := StartRepositorySpan(ctx, "meter_provider_mapping", "create", map[string]interface{}{
		"meter_id":      mapping.MeterID,
		"provider_type": mapping.ProviderType,
	})
	defer FinishSpan(span)

	if err := mapping.Validate(); err != nil {
		return err
	}

	client := r.client.Querier(ctx)
	_, err := client.MeterProviderMapping.Create().
		SetID(mapping.ID).
		SetTenantID(mapping.TenantID).
		SetStatus(string(mapping.Status)).
		SetCreatedAt(mapping.CreatedAt).
		SetUpdatedAt(mapping.UpdatedAt).
		SetCreatedBy(mapping.CreatedBy).
		SetUpdatedBy(mapping.UpdatedBy).
		SetEnvironmentID(mapping.EnvironmentID).
		SetMeterID(mapping.MeterID).
		SetProviderType(string(mapping.ProviderType)).
		SetProviderMeterID(mapping.ProviderMeterID).
		SetSyncEnabled(mapping.SyncEnabled).
		SetConfiguration(mapping.Configuration).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *meterProviderMappingRepository) Get(ctx context.Context, id string) (*domainIntegration.MeterProviderMapping, error) {
	span := StartRepositorySpan(ctx, "meter_provider_mapping", "get", map[string]interface{}{"id": id})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	mp, err := client.MeterProviderMapping.Query().
		Where(
			entMPM.ID(id),
			entMPM.TenantID(types.GetTenantID(ctx)),
			entMPM.EnvironmentID(types.GetEnvironmentID(ctx)),
		).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return toDomainMPM(mp), nil
}

func (r *meterProviderMappingRepository) List(ctx context.Context, filter *domainIntegration.MeterProviderMappingFilter) ([]*domainIntegration.MeterProviderMapping, error) {
	if filter == nil {
		filter = &domainIntegration.MeterProviderMappingFilter{}
	}
	client := r.client.Querier(ctx)
	query := client.MeterProviderMapping.Query().
		Where(
			entMPM.TenantID(types.GetTenantID(ctx)),
			entMPM.EnvironmentID(types.GetEnvironmentID(ctx)),
		)
	if len(filter.MeterIDs) > 0 {
		query = query.Where(entMPM.MeterIDIn(filter.MeterIDs...))
	}
	if len(filter.ProviderTypes) > 0 {
		pt := make([]string, len(filter.ProviderTypes))
		for i, v := range filter.ProviderTypes {
			pt[i] = string(v)
		}
		query = query.Where(entMPM.ProviderTypeIn(pt...))
	}
	if len(filter.ProviderMeterIDs) > 0 {
		query = query.Where(entMPM.ProviderMeterIDIn(filter.ProviderMeterIDs...))
	}
	if filter.SyncEnabled != nil {
		query = query.Where(entMPM.SyncEnabled(*filter.SyncEnabled))
	}
	if !filter.IsUnlimited() {
		query = query.Limit(filter.GetLimit()).Offset(filter.GetOffset())
	}
	list, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	res := make([]*domainIntegration.MeterProviderMapping, 0, len(list))
	for _, m := range list {
		res = append(res, toDomainMPM(m))
	}
	return res, nil
}

func (r *meterProviderMappingRepository) Count(ctx context.Context, filter *domainIntegration.MeterProviderMappingFilter) (int, error) {
	if filter == nil {
		filter = &domainIntegration.MeterProviderMappingFilter{}
	}
	client := r.client.Querier(ctx)
	query := client.MeterProviderMapping.Query().
		Where(entMPM.TenantID(types.GetTenantID(ctx)), entMPM.EnvironmentID(types.GetEnvironmentID(ctx)))
	if filter.SyncEnabled != nil {
		query = query.Where(entMPM.SyncEnabled(*filter.SyncEnabled))
	}
	cnt, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return cnt, nil
}

func (r *meterProviderMappingRepository) Update(ctx context.Context, mapping *domainIntegration.MeterProviderMapping) error {
	span := StartRepositorySpan(ctx, "meter_provider_mapping", "update", map[string]interface{}{"id": mapping.ID})
	defer FinishSpan(span)
	client := r.client.Querier(ctx)
	_, err := client.MeterProviderMapping.Update().
		Where(entMPM.ID(mapping.ID), entMPM.TenantID(types.GetTenantID(ctx)), entMPM.EnvironmentID(types.GetEnvironmentID(ctx))).
		SetSyncEnabled(mapping.SyncEnabled).
		SetConfiguration(mapping.Configuration).
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

func (r *meterProviderMappingRepository) Delete(ctx context.Context, mapping *domainIntegration.MeterProviderMapping) error {
	client := r.client.Querier(ctx)
	_, err := client.MeterProviderMapping.Update().
		Where(entMPM.ID(mapping.ID), entMPM.TenantID(types.GetTenantID(ctx)), entMPM.EnvironmentID(types.GetEnvironmentID(ctx))).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return nil
}

// Mapping-specific methods
func (r *meterProviderMappingRepository) GetByMeterAndProvider(ctx context.Context, meterID string, providerType domainIntegration.ProviderType) (*domainIntegration.MeterProviderMapping, error) {
	client := r.client.Querier(ctx)
	mp, err := client.MeterProviderMapping.Query().
		Where(entMPM.TenantID(types.GetTenantID(ctx)), entMPM.EnvironmentID(types.GetEnvironmentID(ctx)), entMPM.MeterID(meterID), entMPM.ProviderType(string(providerType)), entMPM.StatusEQ(string(types.StatusPublished))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return toDomainMPM(mp), nil
}

func (r *meterProviderMappingRepository) GetByProviderMeterID(ctx context.Context, providerType domainIntegration.ProviderType, providerMeterID string) (*domainIntegration.MeterProviderMapping, error) {
	client := r.client.Querier(ctx)
	mp, err := client.MeterProviderMapping.Query().
		Where(entMPM.TenantID(types.GetTenantID(ctx)), entMPM.EnvironmentID(types.GetEnvironmentID(ctx)), entMPM.ProviderType(string(providerType)), entMPM.ProviderMeterID(providerMeterID), entMPM.StatusEQ(string(types.StatusPublished))).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return toDomainMPM(mp), nil
}

func (r *meterProviderMappingRepository) ListByProvider(ctx context.Context, providerType domainIntegration.ProviderType, filter *domainIntegration.MeterProviderMappingFilter) ([]*domainIntegration.MeterProviderMapping, error) {
	if filter == nil {
		filter = &domainIntegration.MeterProviderMappingFilter{}
	}
	filter.ProviderTypes = []domainIntegration.ProviderType{providerType}
	return r.List(ctx, filter)
}

func (r *meterProviderMappingRepository) ListEnabledMappings(ctx context.Context, filter *domainIntegration.MeterProviderMappingFilter) ([]*domainIntegration.MeterProviderMapping, error) {
	enabled := true
	if filter == nil {
		filter = &domainIntegration.MeterProviderMappingFilter{}
	}
	filter.SyncEnabled = &enabled
	return r.List(ctx, filter)
}

func (r *meterProviderMappingRepository) BulkCreate(ctx context.Context, mappings []*domainIntegration.MeterProviderMapping) error {
	if len(mappings) == 0 {
		return nil
	}
	client := r.client.Querier(ctx)
	builders := make([]*ent.MeterProviderMappingCreate, 0, len(mappings))
	for _, m := range mappings {
		if err := m.Validate(); err != nil {
			return err
		}
		builders = append(builders, client.MeterProviderMapping.Create().
			SetID(m.ID).
			SetTenantID(m.TenantID).
			SetStatus(string(m.Status)).
			SetCreatedAt(m.CreatedAt).
			SetUpdatedAt(m.UpdatedAt).
			SetCreatedBy(m.CreatedBy).
			SetUpdatedBy(m.UpdatedBy).
			SetEnvironmentID(m.EnvironmentID).
			SetMeterID(m.MeterID).
			SetProviderType(string(m.ProviderType)).
			SetProviderMeterID(m.ProviderMeterID).
			SetSyncEnabled(m.SyncEnabled).
			SetConfiguration(m.Configuration))
	}
	if err := client.MeterProviderMapping.CreateBulk(builders...).Exec(ctx); err != nil {
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return nil
}

func toDomainMPM(mp *ent.MeterProviderMapping) *domainIntegration.MeterProviderMapping {
	if mp == nil {
		return nil
	}
	return &domainIntegration.MeterProviderMapping{
		ID:              mp.ID,
		MeterID:         mp.MeterID,
		ProviderType:    domainIntegration.ProviderType(mp.ProviderType),
		ProviderMeterID: mp.ProviderMeterID,
		SyncEnabled:     mp.SyncEnabled,
		Configuration:   mp.Configuration,
		EnvironmentID:   mp.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  mp.TenantID,
			Status:    types.Status(mp.Status),
			CreatedAt: mp.CreatedAt,
			UpdatedAt: mp.UpdatedAt,
			CreatedBy: mp.CreatedBy,
			UpdatedBy: mp.UpdatedBy,
		},
	}
}
