package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entMapping "github.com/flexprice/flexprice/ent/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/cache"
	domainIntegration "github.com/flexprice/flexprice/internal/domain/integration"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type customerIntegrationMappingRepository struct {
	client postgres.IClient
	log    *logger.Logger
	cache  cache.Cache
}

func NewCustomerIntegrationMappingRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainIntegration.EntityIntegrationMappingRepository {
	return &customerIntegrationMappingRepository{
		client: client,
		log:    log,
		cache:  cache,
	}
}

// Basic CRUD operations - placeholder implementations until Ent schemas are generated
func (r *customerIntegrationMappingRepository) Create(ctx context.Context, mapping *domainIntegration.EntityIntegrationMapping) error {
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "create", map[string]interface{}{
		"entity_id":     mapping.EntityID,
		"provider_type": mapping.ProviderType,
	})
	defer FinishSpan(span)

	if err := mapping.Validate(); err != nil {
		return err
	}

	client := r.client.Querier(ctx)

	_, err := client.EntityIntegrationMapping.Create().
		SetID(mapping.ID).
		SetTenantID(mapping.TenantID).
		SetStatus(string(mapping.Status)).
		SetCreatedAt(mapping.CreatedAt).
		SetUpdatedAt(mapping.UpdatedAt).
		SetCreatedBy(mapping.CreatedBy).
		SetUpdatedBy(mapping.UpdatedBy).
		SetEnvironmentID(mapping.EnvironmentID).
		SetEntityID(mapping.EntityID).
		SetEntityType(string(mapping.EntityType)).
		SetProviderType(string(mapping.ProviderType)).
		SetProviderEntityID(mapping.ProviderEntityID).
		SetMetadata(mapping.Metadata).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).WithHint("failed to create entity integration mapping").Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *customerIntegrationMappingRepository) Get(ctx context.Context, id string) (*domainIntegration.EntityIntegrationMapping, error) {
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "get", map[string]interface{}{
		"mapping_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	em, err := client.EntityIntegrationMapping.Query().
		Where(
			entMapping.ID(id),
			entMapping.TenantID(types.GetTenantID(ctx)),
			entMapping.EnvironmentID(types.GetEnvironmentID(ctx)),
		).Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return toDomainEntityIntegrationMapping(em), nil
}

func (r *customerIntegrationMappingRepository) List(ctx context.Context, filter *domainIntegration.EntityIntegrationMappingFilter) ([]*domainIntegration.EntityIntegrationMapping, error) {
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "list", nil)
	defer FinishSpan(span)

	if filter == nil {
		filter = &domainIntegration.EntityIntegrationMappingFilter{}
	}

	client := r.client.Querier(ctx)
	query := client.EntityIntegrationMapping.Query().
		Where(
			entMapping.TenantID(types.GetTenantID(ctx)),
			entMapping.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	if len(filter.EntityIDs) > 0 {
		query = query.Where(entMapping.EntityIDIn(filter.EntityIDs...))
	}
	if len(filter.EntityTypes) > 0 {
		et := make([]string, len(filter.EntityTypes))
		for i, v := range filter.EntityTypes {
			et[i] = string(v)
		}
		query = query.Where(entMapping.EntityTypeIn(et...))
	}
	if len(filter.ProviderTypes) > 0 {
		pt := make([]string, len(filter.ProviderTypes))
		for i, v := range filter.ProviderTypes {
			pt[i] = string(v)
		}
		query = query.Where(entMapping.ProviderTypeIn(pt...))
	}
	if len(filter.ProviderEntityIDs) > 0 {
		query = query.Where(entMapping.ProviderEntityIDIn(filter.ProviderEntityIDs...))
	}

	// Status filter
	if status := filter.GetStatus(); status != "" {
		query = query.Where(entMapping.StatusEQ(status))
	} else {
		query = query.Where(entMapping.StatusEQ(string(types.StatusPublished)))
	}

	// Pagination
	if !filter.IsUnlimited() {
		query = query.Limit(filter.GetLimit()).Offset(filter.GetOffset())
	}

	// Sorting
	field := filter.GetSort()
	if field == "" {
		field = "created_at"
	}
	order := filter.GetOrder()
	if order == "asc" {
		switch field {
		case "created_at":
			query = query.Order(ent.Asc(entMapping.FieldCreatedAt))
		case "updated_at":
			query = query.Order(ent.Asc(entMapping.FieldUpdatedAt))
		default:
			query = query.Order(ent.Asc(entMapping.FieldCreatedAt))
		}
	} else {
		switch field {
		case "created_at":
			query = query.Order(ent.Desc(entMapping.FieldCreatedAt))
		case "updated_at":
			query = query.Order(ent.Desc(entMapping.FieldUpdatedAt))
		default:
			query = query.Order(ent.Desc(entMapping.FieldCreatedAt))
		}
	}

	list, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}

	res := make([]*domainIntegration.EntityIntegrationMapping, 0, len(list))
	for _, em := range list {
		res = append(res, toDomainEntityIntegrationMapping(em))
	}
	SetSpanSuccess(span)
	return res, nil
}

func (r *customerIntegrationMappingRepository) Count(ctx context.Context, filter *domainIntegration.EntityIntegrationMappingFilter) (int, error) {
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "count", nil)
	defer FinishSpan(span)

	if filter == nil {
		filter = &domainIntegration.EntityIntegrationMappingFilter{}
	}

	client := r.client.Querier(ctx)
	query := client.EntityIntegrationMapping.Query().
		Where(
			entMapping.TenantID(types.GetTenantID(ctx)),
			entMapping.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	if len(filter.EntityIDs) > 0 {
		query = query.Where(entMapping.EntityIDIn(filter.EntityIDs...))
	}
	if len(filter.EntityTypes) > 0 {
		et := make([]string, len(filter.EntityTypes))
		for i, v := range filter.EntityTypes {
			et[i] = string(v)
		}
		query = query.Where(entMapping.EntityTypeIn(et...))
	}
	if len(filter.ProviderTypes) > 0 {
		pt := make([]string, len(filter.ProviderTypes))
		for i, v := range filter.ProviderTypes {
			pt[i] = string(v)
		}
		query = query.Where(entMapping.ProviderTypeIn(pt...))
	}
	if len(filter.ProviderEntityIDs) > 0 {
		query = query.Where(entMapping.ProviderEntityIDIn(filter.ProviderEntityIDs...))
	}

	if status := filter.GetStatus(); status != "" {
		query = query.Where(entMapping.StatusEQ(status))
	} else {
		query = query.Where(entMapping.StatusEQ(string(types.StatusPublished)))
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return count, nil
}

func (r *customerIntegrationMappingRepository) Update(ctx context.Context, mapping *domainIntegration.EntityIntegrationMapping) error {
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "update", map[string]interface{}{
		"mapping_id": mapping.ID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	_, err := client.EntityIntegrationMapping.Update().
		Where(
			entMapping.ID(mapping.ID),
			entMapping.TenantID(types.GetTenantID(ctx)),
			entMapping.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetMetadata(mapping.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *customerIntegrationMappingRepository) Delete(ctx context.Context, mapping *domainIntegration.EntityIntegrationMapping) error {
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "delete", map[string]interface{}{
		"mapping_id": mapping.ID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	_, err := client.EntityIntegrationMapping.Update().
		Where(
			entMapping.ID(mapping.ID),
			entMapping.TenantID(types.GetTenantID(ctx)),
			entMapping.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

// Integration-specific methods
func (r *customerIntegrationMappingRepository) GetByEntityAndProvider(ctx context.Context, entityID string, entityType domainIntegration.EntityType, providerType domainIntegration.ProviderType) (*domainIntegration.EntityIntegrationMapping, error) {
	client := r.client.Querier(ctx)
	em, err := client.EntityIntegrationMapping.Query().
		Where(
			entMapping.TenantID(types.GetTenantID(ctx)),
			entMapping.EnvironmentID(types.GetEnvironmentID(ctx)),
			entMapping.EntityID(entityID),
			entMapping.EntityType(string(entityType)),
			entMapping.ProviderType(string(providerType)),
			entMapping.StatusEQ(string(types.StatusPublished)),
		).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return toDomainEntityIntegrationMapping(em), nil
}

func (r *customerIntegrationMappingRepository) GetByProviderEntityID(ctx context.Context, providerType domainIntegration.ProviderType, providerEntityID string) (*domainIntegration.EntityIntegrationMapping, error) {
	client := r.client.Querier(ctx)
	em, err := client.EntityIntegrationMapping.Query().
		Where(
			entMapping.TenantID(types.GetTenantID(ctx)),
			entMapping.EnvironmentID(types.GetEnvironmentID(ctx)),
			entMapping.ProviderType(string(providerType)),
			entMapping.ProviderEntityID(providerEntityID),
			entMapping.StatusEQ(string(types.StatusPublished)),
		).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	return toDomainEntityIntegrationMapping(em), nil
}

func (r *customerIntegrationMappingRepository) ListByProvider(ctx context.Context, providerType domainIntegration.ProviderType, filter *domainIntegration.EntityIntegrationMappingFilter) ([]*domainIntegration.EntityIntegrationMapping, error) {
	if filter == nil {
		filter = &domainIntegration.EntityIntegrationMappingFilter{}
	}
	filter.ProviderTypes = []domainIntegration.ProviderType{providerType}
	return r.List(ctx, filter)
}

func (r *customerIntegrationMappingRepository) ListByEntityType(ctx context.Context, entityType domainIntegration.EntityType, filter *domainIntegration.EntityIntegrationMappingFilter) ([]*domainIntegration.EntityIntegrationMapping, error) {
	if filter == nil {
		filter = &domainIntegration.EntityIntegrationMappingFilter{}
	}
	filter.EntityTypes = []domainIntegration.EntityType{entityType}
	return r.List(ctx, filter)
}

func (r *customerIntegrationMappingRepository) BulkCreate(ctx context.Context, mappings []*domainIntegration.EntityIntegrationMapping) error {
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "bulk_create", map[string]interface{}{
		"count": len(mappings),
	})
	defer FinishSpan(span)

	if len(mappings) == 0 {
		return nil
	}

	client := r.client.Querier(ctx)
	builders := make([]*ent.EntityIntegrationMappingCreate, 0, len(mappings))
	for _, m := range mappings {
		if err := m.Validate(); err != nil {
			return err
		}
		builders = append(builders, client.EntityIntegrationMapping.Create().
			SetID(m.ID).
			SetTenantID(m.TenantID).
			SetStatus(string(m.Status)).
			SetCreatedAt(m.CreatedAt).
			SetUpdatedAt(m.UpdatedAt).
			SetCreatedBy(m.CreatedBy).
			SetUpdatedBy(m.UpdatedBy).
			SetEnvironmentID(m.EnvironmentID).
			SetEntityID(m.EntityID).
			SetEntityType(string(m.EntityType)).
			SetProviderType(string(m.ProviderType)).
			SetProviderEntityID(m.ProviderEntityID).
			SetMetadata(m.Metadata))
	}

	if err := client.EntityIntegrationMapping.CreateBulk(builders...).Exec(ctx); err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

// Helper converter
func toDomainEntityIntegrationMapping(em *ent.EntityIntegrationMapping) *domainIntegration.EntityIntegrationMapping {
	if em == nil {
		return nil
	}
	return &domainIntegration.EntityIntegrationMapping{
		ID:               em.ID,
		EntityID:         em.EntityID,
		EntityType:       domainIntegration.EntityType(em.EntityType),
		ProviderType:     domainIntegration.ProviderType(em.ProviderType),
		ProviderEntityID: em.ProviderEntityID,
		EnvironmentID:    em.EnvironmentID,
		Metadata:         em.Metadata,
		BaseModel: types.BaseModel{
			TenantID:  em.TenantID,
			Status:    types.Status(em.Status),
			CreatedAt: em.CreatedAt,
			UpdatedAt: em.UpdatedAt,
			CreatedBy: em.CreatedBy,
			UpdatedBy: em.UpdatedBy,
		},
	}
}

// Backward compatibility methods
func (r *customerIntegrationMappingRepository) GetByCustomerAndProvider(ctx context.Context, customerID string, providerType domainIntegration.ProviderType) (*domainIntegration.EntityIntegrationMapping, error) {
	return r.GetByEntityAndProvider(ctx, customerID, domainIntegration.EntityTypeCustomer, providerType)
}

func (r *customerIntegrationMappingRepository) GetByProviderCustomerID(ctx context.Context, providerType domainIntegration.ProviderType, providerCustomerID string) (*domainIntegration.EntityIntegrationMapping, error) {
	return r.GetByProviderEntityID(ctx, providerType, providerCustomerID)
}

func (r *customerIntegrationMappingRepository) ListCustomerMappings(ctx context.Context, filter *domainIntegration.EntityIntegrationMappingFilter) ([]*domainIntegration.EntityIntegrationMapping, error) {
	return r.ListByEntityType(ctx, domainIntegration.EntityTypeCustomer, filter)
}
