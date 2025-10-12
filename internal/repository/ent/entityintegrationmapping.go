package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/entityintegrationmapping"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/cache"
	domainEntityIntegrationMapping "github.com/flexprice/flexprice/internal/domain/entityintegrationmapping"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type entityIntegrationMappingRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts EntityIntegrationMappingQueryOptions
	cache     cache.Cache
}

func NewEntityIntegrationMappingRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainEntityIntegrationMapping.Repository {
	return &entityIntegrationMappingRepository{
		client:    client,
		log:       log,
		queryOpts: EntityIntegrationMappingQueryOptions{},
		cache:     cache,
	}
}

func (r *entityIntegrationMappingRepository) Create(ctx context.Context, mapping *domainEntityIntegrationMapping.EntityIntegrationMapping) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating entity integration mapping",
		"mapping_id", mapping.ID,
		"entity_id", mapping.EntityID,
		"entity_type", mapping.EntityType,
		"provider_type", mapping.ProviderType,
		"provider_entity_id", mapping.ProviderEntityID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "create", map[string]interface{}{
		"mapping_id":         mapping.ID,
		"entity_id":          mapping.EntityID,
		"entity_type":        mapping.EntityType,
		"provider_type":      mapping.ProviderType,
		"provider_entity_id": mapping.ProviderEntityID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if mapping.EnvironmentID == "" {
		mapping.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	entMapping, err := client.EntityIntegrationMapping.Create().
		SetID(mapping.ID).
		SetTenantID(mapping.TenantID).
		SetEntityID(mapping.EntityID).
		SetEntityType(string(mapping.EntityType)).
		SetProviderType(mapping.ProviderType).
		SetProviderEntityID(mapping.ProviderEntityID).
		SetMetadata(mapping.Metadata).
		SetStatus(string(mapping.Status)).
		SetCreatedAt(mapping.CreatedAt).
		SetUpdatedAt(mapping.UpdatedAt).
		SetCreatedBy(mapping.CreatedBy).
		SetUpdatedBy(mapping.UpdatedBy).
		SetEnvironmentID(mapping.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Idx_entity_integration_mapping_unique {
					return ierr.WithError(err).
						WithHint("A mapping for this entity and provider already exists").
						WithReportableDetails(map[string]any{
							"entity_id":     mapping.EntityID,
							"entity_type":   mapping.EntityType,
							"provider_type": mapping.ProviderType,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
			return ierr.WithError(err).
				WithHint("Failed to create entity integration mapping").
				WithReportableDetails(map[string]any{
					"entity_id":          mapping.EntityID,
					"entity_type":        mapping.EntityType,
					"provider_type":      mapping.ProviderType,
					"provider_entity_id": mapping.ProviderEntityID,
				}).
				Mark(ierr.ErrValidation)
		}

		return ierr.WithError(err).
			WithHint("Failed to create entity integration mapping").
			Mark(ierr.ErrInternal)
	}

	// Update the domain object with the created entity
	*mapping = *domainEntityIntegrationMapping.FromEnt(entMapping)

	// Set cache
	r.SetCache(ctx, mapping)

	return nil
}

func (r *entityIntegrationMappingRepository) Get(ctx context.Context, id string) (*domainEntityIntegrationMapping.EntityIntegrationMapping, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting entity integration mapping", "mapping_id", id)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "get", map[string]interface{}{
		"mapping_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cached := r.GetCache(ctx, id); cached != nil {
		return cached, nil
	}

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	mapping, err := client.EntityIntegrationMapping.Query().
		Where(
			entityintegrationmapping.ID(id),
			entityintegrationmapping.TenantID(tenantID),
			entityintegrationmapping.EnvironmentID(environmentID),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.NewError("entity integration mapping not found").
				WithHint("The specified entity integration mapping does not exist").
				WithReportableDetails(map[string]any{
					"mapping_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}

		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve entity integration mapping").
			Mark(ierr.ErrInternal)
	}

	domainMapping := domainEntityIntegrationMapping.FromEnt(mapping)

	// Set cache
	r.SetCache(ctx, domainMapping)

	return domainMapping, nil
}

func (r *entityIntegrationMappingRepository) List(ctx context.Context, filter *types.EntityIntegrationMappingFilter) ([]*domainEntityIntegrationMapping.EntityIntegrationMapping, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("listing entity integration mappings", "filter", filter)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.EntityIntegrationMapping.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = ApplyPagination(query, filter, r.queryOpts)
	query = ApplySorting(query, filter, r.queryOpts)

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	mappings, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list entity integration mappings").
			Mark(ierr.ErrInternal)
	}

	return domainEntityIntegrationMapping.FromEntList(mappings), nil
}

func (r *entityIntegrationMappingRepository) Count(ctx context.Context, filter *types.EntityIntegrationMappingFilter) (int, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("counting entity integration mappings", "filter", filter)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.EntityIntegrationMapping.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count entity integration mappings").
			Mark(ierr.ErrInternal)
	}

	return count, nil
}

func (r *entityIntegrationMappingRepository) Update(ctx context.Context, mapping *domainEntityIntegrationMapping.EntityIntegrationMapping) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating entity integration mapping",
		"mapping_id", mapping.ID,
		"entity_id", mapping.EntityID,
		"entity_type", mapping.EntityType,
		"provider_type", mapping.ProviderType,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "update", map[string]interface{}{
		"mapping_id": mapping.ID,
	})
	defer FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Get the existing mapping
	existingMapping, err := client.EntityIntegrationMapping.Query().
		Where(
			entityintegrationmapping.ID(mapping.ID),
			entityintegrationmapping.TenantID(tenantID),
			entityintegrationmapping.EnvironmentID(environmentID),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.NewError("entity integration mapping not found").
				WithHint("The specified entity integration mapping does not exist").
				WithReportableDetails(map[string]any{
					"mapping_id": mapping.ID,
				}).
				Mark(ierr.ErrNotFound)
		}

		return ierr.WithError(err).
			WithHint("Failed to retrieve entity integration mapping for update").
			Mark(ierr.ErrInternal)
	}

	// Update the mapping
	updateQuery := client.EntityIntegrationMapping.UpdateOne(existingMapping).
		SetEntityID(mapping.EntityID).
		SetEntityType(string(mapping.EntityType)).
		SetProviderType(mapping.ProviderType).
		SetProviderEntityID(mapping.ProviderEntityID).
		SetMetadata(mapping.Metadata).
		SetStatus(string(mapping.Status)).
		SetUpdatedAt(mapping.UpdatedAt).
		SetUpdatedBy(mapping.UpdatedBy)

	updatedMapping, err := updateQuery.Save(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Idx_entity_integration_mapping_unique {
					return ierr.WithError(err).
						WithHint("A mapping for this entity and provider already exists").
						WithReportableDetails(map[string]any{
							"entity_id":     mapping.EntityID,
							"entity_type":   mapping.EntityType,
							"provider_type": mapping.ProviderType,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
			return ierr.WithError(err).
				WithHint("Failed to update entity integration mapping").
				WithReportableDetails(map[string]any{
					"entity_id":          mapping.EntityID,
					"entity_type":        mapping.EntityType,
					"provider_type":      mapping.ProviderType,
					"provider_entity_id": mapping.ProviderEntityID,
				}).
				Mark(ierr.ErrValidation)
		}

		return ierr.WithError(err).
			WithHint("Failed to update entity integration mapping").
			Mark(ierr.ErrInternal)
	}

	// Update the domain object with the updated entity
	*mapping = *domainEntityIntegrationMapping.FromEnt(updatedMapping)

	// Update cache
	r.SetCache(ctx, mapping)

	return nil
}

func (r *entityIntegrationMappingRepository) Delete(ctx context.Context, mapping *domainEntityIntegrationMapping.EntityIntegrationMapping) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting entity integration mapping", "mapping_id", mapping.ID)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entity_integration_mapping", "delete", map[string]interface{}{
		"mapping_id": mapping.ID,
	})
	defer FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	_, err := client.EntityIntegrationMapping.Update().
		Where(
			entityintegrationmapping.ID(mapping.ID),
			entityintegrationmapping.TenantID(tenantID),
			entityintegrationmapping.EnvironmentID(environmentID),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.NewError("entity integration mapping not found").
				WithHint("The specified entity integration mapping does not exist").
				WithReportableDetails(map[string]any{
					"mapping_id": mapping.ID,
				}).
				Mark(ierr.ErrNotFound)
		}

		return ierr.WithError(err).
			WithHint("Failed to delete entity integration mapping").
			Mark(ierr.ErrInternal)
	}

	// Delete cache
	r.DeleteCache(ctx, mapping)

	return nil
}

// Provider-specific queries

// Query options

type EntityIntegrationMappingQuery = *ent.EntityIntegrationMappingQuery

type EntityIntegrationMappingQueryOptions struct{}

func (o EntityIntegrationMappingQueryOptions) ApplyTenantFilter(ctx context.Context, query EntityIntegrationMappingQuery) EntityIntegrationMappingQuery {
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" {
		query = query.Where(entityintegrationmapping.TenantID(tenantID))
	}
	return query
}

func (o EntityIntegrationMappingQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query EntityIntegrationMappingQuery) EntityIntegrationMappingQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		query = query.Where(entityintegrationmapping.EnvironmentID(environmentID))
	}
	return query
}

func (o EntityIntegrationMappingQueryOptions) ApplyStatusFilter(query EntityIntegrationMappingQuery, status string) EntityIntegrationMappingQuery {
	if status != "" {
		query = query.Where(entityintegrationmapping.Status(status))
	}
	return query
}

func (o EntityIntegrationMappingQueryOptions) ApplySortFilter(query EntityIntegrationMappingQuery, field string, order string) EntityIntegrationMappingQuery {
	if field != "" {
		if order == "asc" {
			query = query.Order(ent.Asc(field))
		} else {
			query = query.Order(ent.Desc(field))
		}
	}
	return query
}

func (o EntityIntegrationMappingQueryOptions) ApplyPaginationFilter(query EntityIntegrationMappingQuery, limit int, offset int) EntityIntegrationMappingQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o EntityIntegrationMappingQueryOptions) GetFieldName(field string) string {
	fieldMap := map[string]string{
		"id":                 "id",
		"entity_id":          "entity_id",
		"entity_type":        "entity_type",
		"provider_type":      "provider_type",
		"provider_entity_id": "provider_entity_id",
		"status":             "status",
		"created_at":         "created_at",
		"updated_at":         "updated_at",
	}

	if mappedField, exists := fieldMap[field]; exists {
		return mappedField
	}

	return field
}

func (o EntityIntegrationMappingQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewError("invalid field name").
			WithHint("Please provide a valid field name").
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o EntityIntegrationMappingQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.EntityIntegrationMappingFilter, query EntityIntegrationMappingQuery) (EntityIntegrationMappingQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	if f.EntityID != "" {
		query = query.Where(entityintegrationmapping.EntityID(f.EntityID))
	}

	if f.EntityType != "" {
		query = query.Where(entityintegrationmapping.EntityType(string(f.EntityType)))
	}

	if len(f.EntityIDs) > 0 {
		query = query.Where(entityintegrationmapping.EntityIDIn(f.EntityIDs...))
	}

	if len(f.ProviderTypes) > 0 {
		query = query.Where(entityintegrationmapping.ProviderTypeIn(f.ProviderTypes...))
	}

	if len(f.ProviderEntityIDs) > 0 {
		query = query.Where(entityintegrationmapping.ProviderEntityIDIn(f.ProviderEntityIDs...))
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[EntityIntegrationMappingQuery, predicate.EntityIntegrationMapping](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.EntityIntegrationMapping { return predicate.EntityIntegrationMapping(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[EntityIntegrationMappingQuery, entityintegrationmapping.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) entityintegrationmapping.OrderOption {
				return entityintegrationmapping.OrderOption(o)
			},
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

// Cache operations

func (r *entityIntegrationMappingRepository) SetCache(ctx context.Context, mapping *domainEntityIntegrationMapping.EntityIntegrationMapping) {
	span := cache.StartCacheSpan(ctx, "entity_integration_mapping", "set", map[string]interface{}{
		"mapping_id": mapping.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	key := cache.GenerateKey(cache.PrefixEntityIntegrationMapping, tenantID, environmentID, mapping.ID)
	r.cache.Set(ctx, key, mapping, cache.ExpiryDefaultInMemory)

	r.log.Debugw("cache set", "key", key)
}

func (r *entityIntegrationMappingRepository) GetCache(ctx context.Context, key string) *domainEntityIntegrationMapping.EntityIntegrationMapping {
	span := cache.StartCacheSpan(ctx, "entity_integration_mapping", "get", map[string]interface{}{
		"key": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cacheKey := cache.GenerateKey(cache.PrefixEntityIntegrationMapping, tenantID, environmentID, key)
	if cached, found := r.cache.Get(ctx, cacheKey); found {
		if mapping, ok := cached.(*domainEntityIntegrationMapping.EntityIntegrationMapping); ok {
			r.log.Debugw("cache hit", "key", cacheKey)
			return mapping
		}
	}

	r.log.Debugw("cache miss", "key", cacheKey)
	return nil
}

func (r *entityIntegrationMappingRepository) DeleteCache(ctx context.Context, mapping *domainEntityIntegrationMapping.EntityIntegrationMapping) {
	span := cache.StartCacheSpan(ctx, "entity_integration_mapping", "delete", map[string]interface{}{
		"mapping_id": mapping.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	key := cache.GenerateKey(cache.PrefixEntityIntegrationMapping, tenantID, environmentID, mapping.ID)
	r.cache.Delete(ctx, key)

	r.log.Debugw("cache deleted", "key", key)
}
