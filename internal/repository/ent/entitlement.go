package ent

import (
	"context"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/entitlement"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domainEntitlement "github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type entitlementRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts EntitlementQueryOptions
	cache     cache.Cache
}

func NewEntitlementRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainEntitlement.Repository {
	return &entitlementRepository{
		client:    client,
		log:       log,
		queryOpts: EntitlementQueryOptions{},
		cache:     cache,
	}
}

func (r *entitlementRepository) Create(ctx context.Context, e *domainEntitlement.Entitlement) (*domainEntitlement.Entitlement, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "create", map[string]interface{}{
		"entity_type": e.EntityType,
		"entity_id":   e.EntityID,
		"feature_id":  e.FeatureID,
		"tenant_id":   e.TenantID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if e.EnvironmentID == "" {
		e.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	result, err := client.Entitlement.Create().
		SetID(e.ID).
		SetEntityType(string(e.EntityType)).
		SetEntityID(e.EntityID).
		SetFeatureID(e.FeatureID).
		SetFeatureType(string(e.FeatureType)).
		SetIsEnabled(e.IsEnabled).
		SetNillableUsageLimit(e.UsageLimit).
		SetUsageResetPeriod(string(e.UsageResetPeriod)).
		SetIsSoftLimit(e.IsSoftLimit).
		SetStaticValue(e.StaticValue).
		SetTenantID(e.TenantID).
		SetStatus(string(e.Status)).
		SetCreatedAt(e.CreatedAt).
		SetUpdatedAt(e.UpdatedAt).
		SetCreatedBy(e.CreatedBy).
		SetUpdatedBy(e.UpdatedBy).
		SetEnvironmentID(e.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return nil, ierr.WithError(err).
				WithHint("An entitlement with this plan and feature already exists").
				WithReportableDetails(map[string]interface{}{
					"entity_type": e.EntityType,
					"entity_id":   e.EntityID,
					"feature_id":  e.FeatureID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to create entitlement").
			WithReportableDetails(map[string]interface{}{
				"entity_type": e.EntityType,
				"entity_id":   e.EntityID,
				"feature_id":  e.FeatureID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainEntitlement.FromEnt(result), nil
}

func (r *entitlementRepository) Get(ctx context.Context, id string) (*domainEntitlement.Entitlement, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "get", map[string]interface{}{
		"entitlement_id": id,
		"tenant_id":      types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedEntitlement := r.GetCache(ctx, id); cachedEntitlement != nil {
		return cachedEntitlement, nil
	}

	client := r.client.Querier(ctx)
	r.log.Debugw("getting entitlement",
		"entitlement_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	result, err := client.Entitlement.Query().
		Where(
			entitlement.ID(id),
			entitlement.TenantID(types.GetTenantID(ctx)),
			entitlement.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Entitlement not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get entitlement").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrDatabase)
	}

	entitlementData := domainEntitlement.FromEnt(result)
	r.SetCache(ctx, entitlementData)
	return entitlementData, nil
}

func (r *entitlementRepository) List(ctx context.Context, filter *types.EntitlementFilter) ([]*domainEntitlement.Entitlement, error) {
	if filter == nil {
		filter = &types.EntitlementFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "list", map[string]interface{}{
		"tenant_id":    types.GetTenantID(ctx),
		"entity_type":  filter.EntityType,
		"entity_ids":   filter.EntityIDs,
		"feature_ids":  filter.FeatureIDs,
		"feature_type": filter.FeatureType,
	})
	defer FinishSpan(span)

	query := client.Entitlement.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	results, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list entitlements").
			WithReportableDetails(map[string]interface{}{
				"entity_type":  filter.EntityType,
				"entity_ids":   filter.EntityIDs,
				"feature_ids":  filter.FeatureIDs,
				"feature_type": filter.FeatureType,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainEntitlement.FromEntList(results), nil
}

func (r *entitlementRepository) Count(ctx context.Context, filter *types.EntitlementFilter) (int, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "count", map[string]interface{}{
		"tenant_id":    types.GetTenantID(ctx),
		"entity_type":  filter.EntityType,
		"entity_ids":   filter.EntityIDs,
		"feature_ids":  filter.FeatureIDs,
		"feature_type": filter.FeatureType,
	})
	defer FinishSpan(span)

	query := client.Entitlement.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count entitlements").
			WithReportableDetails(map[string]interface{}{
				"entity_type":  filter.EntityType,
				"entity_ids":   filter.EntityIDs,
				"feature_ids":  filter.FeatureIDs,
				"feature_type": filter.FeatureType,
			}).
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *entitlementRepository) ListAll(ctx context.Context, filter *types.EntitlementFilter) ([]*domainEntitlement.Entitlement, error) {
	if filter == nil {
		filter = types.NewNoLimitEntitlementFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid entitlement filter").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	return r.List(ctx, filter)
}

func (r *entitlementRepository) Update(ctx context.Context, e *domainEntitlement.Entitlement) (*domainEntitlement.Entitlement, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating entitlement",
		"entitlement_id", e.ID,
		"tenant_id", e.TenantID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "update", map[string]interface{}{
		"entitlement_id": e.ID,
		"tenant_id":      e.TenantID,
		"entity_type":    e.EntityType,
		"entity_id":      e.EntityID,
		"feature_id":     e.FeatureID,
	})
	defer FinishSpan(span)

	result, err := client.Entitlement.UpdateOneID(e.ID).
		Where(
			entitlement.TenantID(e.TenantID),
			entitlement.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetEntityType(string(e.EntityType)).
		SetEntityID(e.EntityID).
		SetFeatureID(e.FeatureID).
		SetFeatureType(string(e.FeatureType)).
		SetIsEnabled(e.IsEnabled).
		SetIsSoftLimit(e.IsSoftLimit).
		SetNillableUsageLimit(e.UsageLimit).
		SetUsageResetPeriod(string(e.UsageResetPeriod)).
		SetStaticValue(e.StaticValue).
		SetStatus(string(e.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Entitlement not found").
				WithReportableDetails(map[string]interface{}{"id": e.ID}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to update entitlement").
			WithReportableDetails(map[string]interface{}{"id": e.ID}).
			Mark(ierr.ErrDatabase)
	}
	r.DeleteCache(ctx, e.ID)
	return domainEntitlement.FromEnt(result), nil
}

func (r *entitlementRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "delete", map[string]interface{}{
		"entitlement_id": id,
		"tenant_id":      types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("deleting entitlement",
		"entitlement_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Entitlement.Update().
		Where(
			entitlement.ID(id),
			entitlement.TenantID(types.GetTenantID(ctx)),
			entitlement.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Entitlement not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete entitlement").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, id)
	return nil
}

func (r *entitlementRepository) CreateBulk(ctx context.Context, entitlements []*domainEntitlement.Entitlement) ([]*domainEntitlement.Entitlement, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "create_bulk", map[string]interface{}{
		"count":     len(entitlements),
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(entitlements) == 0 {
		return []*domainEntitlement.Entitlement{}, nil
	}

	client := r.client.Querier(ctx)
	builders := make([]*ent.EntitlementCreate, len(entitlements))

	// Get environment ID from context
	environmentID := types.GetEnvironmentID(ctx)

	for i, e := range entitlements {
		// Set environment ID from context if not already set
		if e.EnvironmentID == "" {
			e.EnvironmentID = environmentID
		}

		builders[i] = client.Entitlement.Create().
			SetID(e.ID).
			SetEntityType(string(e.EntityType)).
			SetEntityID(e.EntityID).
			SetFeatureID(e.FeatureID).
			SetFeatureType(string(e.FeatureType)).
			SetIsEnabled(e.IsEnabled).
			SetNillableUsageLimit(e.UsageLimit).
			SetUsageResetPeriod(string(e.UsageResetPeriod)).
			SetIsSoftLimit(e.IsSoftLimit).
			SetStaticValue(e.StaticValue).
			SetTenantID(e.TenantID).
			SetStatus(string(e.Status)).
			SetCreatedAt(e.CreatedAt).
			SetUpdatedAt(e.UpdatedAt).
			SetCreatedBy(e.CreatedBy).
			SetUpdatedBy(e.UpdatedBy).
			SetEnvironmentID(e.EnvironmentID)
	}

	results, err := client.Entitlement.CreateBulk(builders...).Save(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to create entitlements in bulk").
			WithReportableDetails(map[string]interface{}{
				"count": len(entitlements),
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainEntitlement.FromEntList(results), nil
}

func (r *entitlementRepository) DeleteBulk(ctx context.Context, ids []string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "delete_bulk", map[string]interface{}{
		"count":     len(ids),
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(ids) == 0 {
		return nil
	}

	r.log.Debugw("deleting entitlements in bulk", "count", len(ids))

	_, err := r.client.Querier(ctx).Entitlement.Update().
		Where(
			entitlement.IDIn(ids...),
			entitlement.TenantID(types.GetTenantID(ctx)),
			entitlement.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete entitlements in bulk").
			WithReportableDetails(map[string]interface{}{
				"count": len(ids),
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// ListByPlanIDs retrieves all entitlements for the given plan IDs
func (r *entitlementRepository) ListByPlanIDs(ctx context.Context, planIDs []string) ([]*domainEntitlement.Entitlement, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "list_by_plan_ids", map[string]interface{}{
		"entity_type": types.ENTITLEMENT_ENTITY_TYPE_PLAN,
		"entity_ids":  planIDs,
		"tenant_id":   types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(planIDs) == 0 {
		return []*domainEntitlement.Entitlement{}, nil
	}

	r.log.Debugw("listing entitlements by plan IDs", "plan_ids", planIDs)

	// Create a filter with plan IDs
	filter := &types.EntitlementFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  lo.ToPtr(types.ENTITLEMENT_ENTITY_TYPE_PLAN),
		EntityIDs:   planIDs,
	}

	// Use the existing List method
	return r.List(ctx, filter)
}

// ListByFeatureIDs retrieves all entitlements for the given feature IDs
func (r *entitlementRepository) ListByFeatureIDs(ctx context.Context, featureIDs []string) ([]*domainEntitlement.Entitlement, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "list_by_feature_ids", map[string]interface{}{
		"feature_ids": featureIDs,
		"tenant_id":   types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(featureIDs) == 0 {
		return []*domainEntitlement.Entitlement{}, nil
	}

	r.log.Debugw("listing entitlements by feature IDs", "feature_ids", featureIDs)

	// Create a filter with feature IDs
	filter := &types.EntitlementFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		FeatureIDs:  featureIDs,
	}

	// Use the existing List method
	return r.List(ctx, filter)
}

// ListByAddonIDs retrieves all entitlements for the given addon IDs
func (r *entitlementRepository) ListByAddonIDs(ctx context.Context, addonIDs []string) ([]*domainEntitlement.Entitlement, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "entitlement", "list_by_addon_ids", map[string]interface{}{
		"entity_type": types.ENTITLEMENT_ENTITY_TYPE_ADDON,
		"entity_ids":  addonIDs,
		"tenant_id":   types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(addonIDs) == 0 {
		return []*domainEntitlement.Entitlement{}, nil
	}

	r.log.Debugw("listing entitlements by addon IDs", "addon_ids", addonIDs)

	// Create a filter with addon IDs
	filter := &types.EntitlementFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		EntityType:  lo.ToPtr(types.ENTITLEMENT_ENTITY_TYPE_ADDON),
		EntityIDs:   addonIDs,
	}

	// Use the existing List method
	return r.List(ctx, filter)
}

// EntitlementQuery type alias for better readability
type EntitlementQuery = *ent.EntitlementQuery

// EntitlementQueryOptions implements BaseQueryOptions for entitlement queries
type EntitlementQueryOptions struct{}

func (o EntitlementQueryOptions) ApplyTenantFilter(ctx context.Context, query EntitlementQuery) EntitlementQuery {
	return query.Where(entitlement.TenantID(types.GetTenantID(ctx)))
}

func (o EntitlementQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query EntitlementQuery) EntitlementQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(entitlement.EnvironmentID(environmentID))
	}
	return query
}

func (o EntitlementQueryOptions) ApplyStatusFilter(query EntitlementQuery, status string) EntitlementQuery {
	if status == "" {
		return query.Where(entitlement.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(entitlement.Status(status))
}

func (o EntitlementQueryOptions) ApplySortFilter(query EntitlementQuery, field string, order string) EntitlementQuery {
	field = o.GetFieldName(field)

	// Special handling for display_order
	if field == entitlement.FieldDisplayOrder {
		// Custom SQL to handle the ordering:
		// 1. First show records WITH display_order (sorted by display_order asc/desc)
		// 2. Then show records with display_order = NULL (sorted by created_at desc)
		query = query.Order(func(s *sql.Selector) {
			if order == types.OrderDesc {
				// For DESC: NOT NULL first in DESC order, then NULLs by created_at
				s.OrderBy("CASE WHEN " + field + " IS NULL THEN 2 ELSE 1 END, " + field + " DESC NULLS LAST, " + entitlement.FieldCreatedAt + " DESC")
			} else {
				// For ASC: NOT NULL first in ASC order, then NULLs by created_at
				s.OrderBy("CASE WHEN " + field + " IS NULL THEN 2 ELSE 1 END, " + field + " ASC NULLS LAST, " + entitlement.FieldCreatedAt + " DESC")
			}
		})
		return query
	}

	// Normal sorting for other fields
	if order == types.OrderDesc {
		return query.Order(ent.Desc(field))
	}
	return query.Order(ent.Asc(field))
}

func (o EntitlementQueryOptions) ApplyPaginationFilter(query EntitlementQuery, limit int, offset int) EntitlementQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o EntitlementQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return entitlement.FieldCreatedAt
	case "updated_at":
		return entitlement.FieldUpdatedAt
	case "display_order":
		return entitlement.FieldDisplayOrder
	default:
		return field
	}
}

func (o EntitlementQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in entitlement query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o EntitlementQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.EntitlementFilter, query EntitlementQuery) (EntitlementQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply entity ID filter if specified
	if len(f.EntityIDs) > 0 {
		query = query.Where(entitlement.EntityIDIn(f.EntityIDs...))
	}

	// Apply feature IDs filter if specified
	if len(f.FeatureIDs) > 0 {
		query = query.Where(entitlement.FeatureIDIn(f.FeatureIDs...))
	}

	// Apply feature type filter if specified
	if f.FeatureType != nil {
		query = query.Where(entitlement.FeatureType(string(*f.FeatureType)))
	}

	// Apply is_enabled filter if specified
	if f.IsEnabled != nil {
		query = query.Where(entitlement.IsEnabled(*f.IsEnabled))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(entitlement.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(entitlement.CreatedAtLTE(*f.EndTime))
		}
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[EntitlementQuery, predicate.Entitlement](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Entitlement { return predicate.Entitlement(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[EntitlementQuery, entitlement.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) entitlement.OrderOption { return entitlement.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (r *entitlementRepository) SetCache(ctx context.Context, entitlement *domainEntitlement.Entitlement) {
	span := cache.StartCacheSpan(ctx, "entitlement", "set", map[string]interface{}{
		"entitlement_id": entitlement.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixEntitlement, tenantID, environmentID, entitlement.ID)
	r.cache.Set(ctx, cacheKey, entitlement, cache.ExpiryDefaultInMemory)
}

func (r *entitlementRepository) GetCache(ctx context.Context, key string) *domainEntitlement.Entitlement {
	span := cache.StartCacheSpan(ctx, "entitlement", "get", map[string]interface{}{
		"entitlement_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixEntitlement, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainEntitlement.Entitlement)
	}
	return nil
}

func (r *entitlementRepository) DeleteCache(ctx context.Context, entitlementID string) {
	span := cache.StartCacheSpan(ctx, "entitlement", "delete", map[string]interface{}{
		"entitlement_id": entitlementID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixEntitlement, tenantID, environmentID, entitlementID)
	r.cache.Delete(ctx, cacheKey)
}
