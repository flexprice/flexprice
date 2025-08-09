package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/addon"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/cache"
	domainAddon "github.com/flexprice/flexprice/internal/domain/addon"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type addonRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts AddonQueryOptions
	cache     cache.Cache
}

func NewAddonRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainAddon.Repository {
	return &addonRepository{
		client:    client,
		log:       log,
		queryOpts: AddonQueryOptions{},
		cache:     cache,
	}
}

func (r *addonRepository) Create(ctx context.Context, a *domainAddon.Addon) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating addon",
		"addon_id", a.ID,
		"tenant_id", a.TenantID,
		"name", a.Name,
		"lookup_key", a.LookupKey,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "create", map[string]interface{}{
		"addon_id":   a.ID,
		"name":       a.Name,
		"lookup_key": a.LookupKey,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.Addon.Create().
		SetID(a.ID).
		SetTenantID(a.TenantID).
		SetEnvironmentID(a.EnvironmentID).
		SetStatus(string(a.Status)).
		SetName(a.Name).
		SetDescription(a.Description).
		SetType(string(a.Type)).
		SetLookupKey(a.LookupKey).
		SetMetadata(a.Metadata).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetCreatedAt(a.CreatedAt).
		SetUpdatedAt(a.UpdatedAt).
		SetEnvironmentID(a.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.AddonTenantIDEnvironmentIDLookupKeyConstraint {
					return ierr.WithError(err).
						WithHint("Addon with same lookup key already exists").
						WithReportableDetails(map[string]any{
							"lookup_key": a.LookupKey,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
			return ierr.WithError(err).
				WithHint("Addon with same tenant, environment and lookup key already exists").
				WithReportableDetails(map[string]any{
					"addon_id": a.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create addon").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *addonRepository) GetByID(ctx context.Context, id string) (*domainAddon.Addon, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "get", map[string]interface{}{
		"addon_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedAddon := r.GetCache(ctx, id); cachedAddon != nil {
		return cachedAddon, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting addon",
		"addon_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	a, err := client.Addon.Query().
		Where(
			addon.ID(id),
			addon.TenantID(types.GetTenantID(ctx)),
			addon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Addon with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"addon_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get addon with ID %s", id).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	addonData := domainAddon.FromEnt(a)
	r.SetCache(ctx, addonData)
	return addonData, nil
}

func (r *addonRepository) GetByLookupKey(ctx context.Context, lookupKey string) (*domainAddon.Addon, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "get_by_lookup_key", map[string]interface{}{
		"lookup_key": lookupKey,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedAddon := r.GetCache(ctx, lookupKey); cachedAddon != nil {
		return cachedAddon, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting addon by lookup key",
		"lookup_key", lookupKey,
	)

	a, err := client.Addon.Query().
		Where(
			addon.LookupKey(lookupKey),
			addon.TenantID(types.GetTenantID(ctx)),
			addon.Status(string(types.StatusPublished)),
			addon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Addon with lookup key %s was not found", lookupKey).
				WithReportableDetails(map[string]any{
					"lookup_key": lookupKey,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get addon by lookup key").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAddon.FromEnt(a), nil
}

func (r *addonRepository) List(ctx context.Context, filter *types.AddonFilter) ([]*domainAddon.Addon, error) {
	if filter == nil {
		filter = &types.AddonFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	query := client.Addon.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return nil, err
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	addons, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list addons").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainAddon.FromEntList(addons), nil
}

func (r *addonRepository) Count(ctx context.Context, filter *types.AddonFilter) (int, error) {
	client := r.client.Querier(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Addon.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count addons").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *addonRepository) ListAll(ctx context.Context, filter *types.AddonFilter) ([]*domainAddon.Addon, error) {
	if filter == nil {
		filter = types.NewNoLimitAddonFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	return r.List(ctx, filter)
}

func (r *addonRepository) Update(ctx context.Context, a *domainAddon.Addon) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating addon",
		"addon_id", a.ID,
		"tenant_id", a.TenantID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "update", map[string]interface{}{
		"addon_id": a.ID,
	})
	defer FinishSpan(span)

	_, err := client.Addon.Update().
		Where(
			addon.ID(a.ID),
			addon.TenantID(a.TenantID),
			addon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(a.Name).
		SetDescription(a.Description).
		SetStatus(string(a.Status)).
		SetType(string(a.Type)).
		SetMetadata(a.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Addon with ID %s was not found", a.ID).
				WithReportableDetails(map[string]any{
					"addon_id": a.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update addon").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, a.ID)
	return nil
}

func (r *addonRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting addon",
		"addon_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "delete", map[string]interface{}{
		"addon_id": id,
	})
	defer FinishSpan(span)

	_, err := client.Addon.Update().
		Where(
			addon.ID(id),
			addon.TenantID(types.GetTenantID(ctx)),
			addon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Addon with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"addon_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete addon").
			WithReportableDetails(map[string]any{
				"addon_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, id)
	return nil
}

// ListByIDs retrieves addons by their IDs
func (r *addonRepository) ListByIDs(ctx context.Context, addonIDs []string) ([]*domainAddon.Addon, error) {
	if len(addonIDs) == 0 {
		return []*domainAddon.Addon{}, nil
	}

	r.log.Debugw("listing addons by IDs", "addon_ids", addonIDs)

	// Create a filter with addon IDs
	filter := &types.AddonFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		AddonIDs:    addonIDs,
	}

	// Use the existing List method
	return r.List(ctx, filter)
}

// ListSubscriptionAddons retrieves all addons for a subscription

// AddonQuery type alias for better readability
type AddonQuery = *ent.AddonQuery

// AddonQueryOptions implements query options for addon filtering and sorting
type AddonQueryOptions struct{}

func (o AddonQueryOptions) ApplyTenantFilter(ctx context.Context, query AddonQuery) AddonQuery {
	return query.Where(addon.TenantID(types.GetTenantID(ctx)))
}

func (o AddonQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query AddonQuery) AddonQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(addon.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o AddonQueryOptions) ApplyStatusFilter(query AddonQuery, status string) AddonQuery {
	if status == "" {
		return query.Where(addon.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(addon.Status(status))
}

func (o AddonQueryOptions) ApplySortFilter(query AddonQuery, field string, order string) AddonQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o AddonQueryOptions) ApplyPaginationFilter(query AddonQuery, limit int, offset int) AddonQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o AddonQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return addon.FieldCreatedAt
	case "updated_at":
		return addon.FieldUpdatedAt
	case "name":
		return addon.FieldName
	case "lookup_key":
		return addon.FieldLookupKey
	case "type":
		return addon.FieldType
	case "status":
		return addon.FieldStatus
	default:
		// unknown field
		return ""
	}
}

func (o AddonQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.AddonFilter, query AddonQuery) (AddonQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply addon IDs filter if specified
	if len(f.AddonIDs) > 0 {
		query = query.Where(addon.IDIn(f.AddonIDs...))
	}

	// Apply addon type filter if specified
	if f.AddonType != "" {
		query = query.Where(addon.Type(string(f.AddonType)))
	}

	// Apply lookup keys filter if specified
	if len(f.LookupKeys) > 0 {
		query = query.Where(addon.LookupKeyIn(f.LookupKeys...))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(addon.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(addon.CreatedAtLTE(*f.EndTime))
		}
	}

	// Apply filters using the generic function
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[AddonQuery, predicate.Addon](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Addon { return predicate.Addon(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[AddonQuery, addon.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) addon.OrderOption { return addon.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (o AddonQueryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in addon query", st).
			WithHintf("Unknown field name '%s' in addon query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (r *addonRepository) SetCache(ctx context.Context, addon *domainAddon.Addon) {
	span := cache.StartCacheSpan(ctx, "addon", "set", map[string]interface{}{
		"addon_id": addon.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixAddon, tenantID, environmentID, addon.ID)
	r.cache.Set(ctx, cacheKey, addon, cache.ExpiryDefaultInMemory)
}

func (r *addonRepository) GetCache(ctx context.Context, key string) *domainAddon.Addon {
	span := cache.StartCacheSpan(ctx, "addon", "get", map[string]interface{}{
		"addon_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixAddon, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainAddon.Addon)
	}
	return nil
}

func (r *addonRepository) DeleteCache(ctx context.Context, addonID string) {
	span := cache.StartCacheSpan(ctx, "addon", "delete", map[string]interface{}{
		"addon_id": addonID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixAddon, tenantID, environmentID, addonID)
	r.cache.Delete(ctx, cacheKey)
}
