package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/addonassociation"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domainAddonAssociation "github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type addonAssociationRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts AddonAssociationQueryOptions
	cache     cache.Cache
}

func NewAddonAssociationRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainAddonAssociation.Repository {
	return &addonAssociationRepository{
		client:    client,
		log:       log,
		queryOpts: AddonAssociationQueryOptions{},
		cache:     cache,
	}
}

func (r *addonAssociationRepository) Create(ctx context.Context, a *domainAddonAssociation.AddonAssociation) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating addon association",
		"addon_association_id", a.ID,
		"addon_id", a.AddonID,
		"entity_id", a.EntityID,
		"entity_type", a.EntityType,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon_association", "create", map[string]interface{}{
		"addon_association_id": a.ID,
		"addon_id":             a.AddonID,
		"entity_id":            a.EntityID,
		"entity_type":          a.EntityType,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.AddonAssociation.Create().
		SetID(a.ID).
		SetTenantID(types.GetTenantID(ctx)).
		SetEnvironmentID(a.EnvironmentID).
		SetEntityID(a.EntityID).
		SetEntityType(string(a.EntityType)).
		SetAddonID(a.AddonID).
		SetNillableStartDate(a.StartDate).
		SetNillableEndDate(a.EndDate).
		SetAddonStatus(string(a.AddonStatus)).
		SetCancellationReason(a.CancellationReason).
		SetNillableCancelledAt(a.CancelledAt).
		SetMetadata(a.Metadata).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetCreatedAt(a.CreatedAt).
		SetUpdatedAt(a.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				return ierr.WithError(err).
					WithHint("Addon association with same entity ID, environment ID and addon ID already exists").
					WithReportableDetails(map[string]any{
						"entity_id":      a.EntityID,
						"environment_id": a.EnvironmentID,
						"addon_id":       a.AddonID,
					}).
					Mark(ierr.ErrAlreadyExists)
			}
			return ierr.WithError(err).
				WithHint("Addon association with same entity ID, environment ID and addon ID already exists").
				WithReportableDetails(map[string]any{
					"entity_id":      a.EntityID,
					"environment_id": a.EnvironmentID,
					"addon_id":       a.AddonID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create addon association").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *addonAssociationRepository) GetByID(ctx context.Context, id string) (*domainAddonAssociation.AddonAssociation, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon_association", "get", map[string]interface{}{
		"addon_association_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedAddonAssociation := r.GetCache(ctx, id); cachedAddonAssociation != nil {
		return cachedAddonAssociation, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting addon association",
		"addon_association_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	a, err := client.AddonAssociation.Query().
		Where(
			addonassociation.ID(id),
			addonassociation.TenantID(types.GetTenantID(ctx)),
			addonassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Addon association with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"addon_association_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get addon association with ID %s", id).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	addonAssociationData := domainAddonAssociation.FromEnt(a)
	r.SetCache(ctx, addonAssociationData)
	return addonAssociationData, nil
}

func (r *addonAssociationRepository) List(ctx context.Context, filter *types.AddonAssociationFilter) ([]*domainAddonAssociation.AddonAssociation, error) {
	if filter == nil {
		filter = &types.AddonAssociationFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon_association", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	query := client.AddonAssociation.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return nil, err
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	addonAssociations, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list addon associations").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAddonAssociation.FromEntList(addonAssociations), nil
}

func (r *addonAssociationRepository) Count(ctx context.Context, filter *types.AddonAssociationFilter) (int, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon_association", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.AddonAssociation.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count addon associations").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *addonAssociationRepository) Update(ctx context.Context, a *domainAddonAssociation.AddonAssociation) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating addon association",
		"addon_association_id", a.ID,
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon_association", "update", map[string]interface{}{
		"addon_association_id": a.ID,
	})
	defer FinishSpan(span)

	_, err := client.AddonAssociation.Update().
		Where(
			addonassociation.ID(a.ID),
			addonassociation.TenantID(types.GetTenantID(ctx)),
			addonassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetNillableStartDate(a.StartDate).
		SetNillableEndDate(a.EndDate).
		SetAddonStatus(string(a.AddonStatus)).
		SetCancellationReason(a.CancellationReason).
		SetNillableCancelledAt(a.CancelledAt).
		SetMetadata(a.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Addon association with ID %s was not found", a.ID).
				WithReportableDetails(map[string]any{
					"addon_association_id": a.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update addon association").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, a.ID)
	return nil
}

func (r *addonAssociationRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting addon association",
		"addon_association_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon_association", "delete", map[string]interface{}{
		"addon_association_id": id,
	})
	defer FinishSpan(span)

	_, err := client.AddonAssociation.Update().
		Where(
			addonassociation.ID(id),
			addonassociation.TenantID(types.GetTenantID(ctx)),
			addonassociation.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Addon association with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"addon_association_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete addon association").
			WithReportableDetails(map[string]any{
				"addon_association_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, id)
	return nil
}

// AddonAssociationQuery type alias for better readability
type AddonAssociationQuery = *ent.AddonAssociationQuery

// AddonAssociationQueryOptions implements query options for addon association filtering and sorting
type AddonAssociationQueryOptions struct{}

func (o AddonAssociationQueryOptions) ApplyTenantFilter(ctx context.Context, query AddonAssociationQuery) AddonAssociationQuery {
	return query.Where(addonassociation.TenantID(types.GetTenantID(ctx)))
}

func (o AddonAssociationQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query AddonAssociationQuery) AddonAssociationQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(addonassociation.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o AddonAssociationQueryOptions) ApplyStatusFilter(query AddonAssociationQuery, status string) AddonAssociationQuery {
	if status == "" {
		return query.Where(addonassociation.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(addonassociation.Status(status))
}

func (o AddonAssociationQueryOptions) ApplySortFilter(query AddonAssociationQuery, field string, order string) AddonAssociationQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o AddonAssociationQueryOptions) ApplyPaginationFilter(query AddonAssociationQuery, limit int, offset int) AddonAssociationQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o AddonAssociationQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return addonassociation.FieldCreatedAt
	case "updated_at":
		return addonassociation.FieldUpdatedAt
	case "entity_id":
		return addonassociation.FieldEntityID
	case "entity_type":
		return addonassociation.FieldEntityType
	case "addon_id":
		return addonassociation.FieldAddonID
	case "addon_status":
		return addonassociation.FieldAddonStatus
	case "start_date":
		return addonassociation.FieldStartDate
	case "end_date":
		return addonassociation.FieldEndDate
	default:
		// unknown field
		return ""
	}
}

func (o AddonAssociationQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.AddonAssociationFilter, query AddonAssociationQuery) (AddonAssociationQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply addon IDs filter if specified
	if len(f.AddonIDs) > 0 {
		query = query.Where(addonassociation.AddonIDIn(f.AddonIDs...))
	}

	// Apply entity type filter if specified
	if f.EntityType != nil {
		query = query.Where(addonassociation.EntityType(string(*f.EntityType)))
	}

	// Apply entity IDs filter if specified
	if len(f.EntityIDs) > 0 {
		query = query.Where(addonassociation.EntityIDIn(f.EntityIDs...))
	}

	// Apply addon status filter if specified
	if f.AddonStatus != nil {
		query = query.Where(addonassociation.AddonStatus(*f.AddonStatus))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(addonassociation.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(addonassociation.CreatedAtLTE(*f.EndTime))
		}
	}

	// Apply filters using the generic function
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[AddonAssociationQuery, predicate.AddonAssociation](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.AddonAssociation { return predicate.AddonAssociation(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[AddonAssociationQuery, addonassociation.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) addonassociation.OrderOption { return addonassociation.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (o AddonAssociationQueryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in addon association query", st).
			WithHintf("Unknown field name '%s' in addon association query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (r *addonAssociationRepository) SetCache(ctx context.Context, addonAssociation *domainAddonAssociation.AddonAssociation) {
	span := cache.StartCacheSpan(ctx, "addon_association", "set", map[string]interface{}{
		"addon_association_id": addonAssociation.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixAddonAssociation, tenantID, environmentID, addonAssociation.ID)
	r.cache.Set(ctx, cacheKey, addonAssociation, cache.ExpiryDefaultInMemory)
}

func (r *addonAssociationRepository) GetCache(ctx context.Context, key string) *domainAddonAssociation.AddonAssociation {
	span := cache.StartCacheSpan(ctx, "addon_association", "get", map[string]interface{}{
		"addon_association_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixAddonAssociation, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainAddonAssociation.AddonAssociation)
	}
	return nil
}

func (r *addonAssociationRepository) DeleteCache(ctx context.Context, addonAssociationID string) {
	span := cache.StartCacheSpan(ctx, "addon_association", "delete", map[string]interface{}{
		"addon_association_id": addonAssociationID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixAddonAssociation, tenantID, environmentID, addonAssociationID)
	r.cache.Delete(ctx, cacheKey)
}
