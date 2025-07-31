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

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "create", map[string]interface{}{
		"addon_id": a.ID,
		"name":     a.Name,
		"type":     a.Type,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Infof("creating addon",
		"addon_id", a.ID,
		"tenant_id", a.TenantID,
		"name", a.Name,
		"lookup_key", a.LookupKey,
		"type", a.Type,
	)

	// Set environment ID from context if not already set
	if a.EnvironmentID == "" {
		a.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.Addon.Create().
		SetID(a.ID).
		SetTenantID(a.TenantID).
		SetEnvironmentID(a.EnvironmentID).
		SetTenantID(a.TenantID).
		SetStatus(string(a.Status)).
		SetName(a.Name).
		SetDescription(a.Description).
		SetType(string(a.Type)).
		SetLookupKey(a.LookupKey).
		SetMetadata(a.Metadata).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now()).
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
							"addon_id":   a.ID,
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
				Mark(ierr.ErrDatabase)
		}
		return ierr.WithError(err).
			WithHint("Failed to create addon").
			WithReportableDetails(map[string]interface{}{
				"addon_id":  a.ID,
				"tenant_id": a.TenantID,
				"name":      a.Name,
				"type":      a.Type,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *addonRepository) GetByID(ctx context.Context, id string) (*domainAddon.Addon, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "get_by_id", map[string]interface{}{
		"addon_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedAddon := r.GetCache(ctx, id); cachedAddon != nil {
		return cachedAddon, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting addon by id",
		"addon_id", id,
	)

	entAddon, err := client.Addon.
		Query().
		Where(
			addon.ID(id),
			addon.TenantID(types.GetTenantID(ctx)),
			addon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Addon not found").
				WithReportableDetails(map[string]interface{}{
					"addon_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get addon").
			WithReportableDetails(map[string]interface{}{
				"addon_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	domainAddon := &domainAddon.Addon{}
	result := domainAddon.FromEnt(entAddon)
	r.SetCache(ctx, result)
	return result, nil
}

func (r *addonRepository) GetByLookupKey(ctx context.Context, lookupKey string) (*domainAddon.Addon, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "get_by_lookup_key", map[string]interface{}{
		"tenant_id":      types.GetTenantID(ctx),
		"environment_id": types.GetEnvironmentID(ctx),
		"lookup_key":     lookupKey,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("getting addon by lookup key",
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
		"lookup_key", lookupKey,
	)

	entAddon, err := client.Addon.
		Query().
		Where(
			addon.TenantID(types.GetTenantID(ctx)),
			addon.EnvironmentIDEQ(types.GetEnvironmentID(ctx)),
			addon.LookupKey(lookupKey),
			addon.StatusEQ(string(types.StatusPublished)),
		).
		First(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Addon not found").
				WithReportableDetails(map[string]interface{}{
					"tenant_id":      types.GetTenantID(ctx),
					"environment_id": types.GetEnvironmentID(ctx),
					"lookup_key":     lookupKey,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get addon").
			WithReportableDetails(map[string]interface{}{
				"tenant_id":      types.GetTenantID(ctx),
				"environment_id": types.GetEnvironmentID(ctx),
				"lookup_key":     lookupKey,
			}).
			Mark(ierr.ErrDatabase)
	}

	domainAddon := &domainAddon.Addon{}
	return domainAddon.FromEnt(entAddon), nil
}

func (r *addonRepository) Update(ctx context.Context, a *domainAddon.Addon) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "update", map[string]interface{}{
		"addon_id": a.ID,
		"name":     a.Name,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("updating addon",
		"addon_id", a.ID,
		"tenant_id", a.TenantID,
		"name", a.Name,
	)

	updateBuilder := client.Addon.
		Update().
		Where(
			addon.ID(a.ID),
			addon.TenantID(types.GetTenantID(ctx)),
			addon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(a.Name).
		SetDescription(a.Description).
		SetMetadata(a.Metadata).
		SetStatus(string(a.Status)).
		SetType(string(a.Type)).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetUpdatedAt(time.Now())

	_, err := updateBuilder.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Addon not found").
				WithReportableDetails(map[string]interface{}{
					"addon_id": a.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update addon").
			WithReportableDetails(map[string]interface{}{
				"addon_id": a.ID,
				"name":     a.Name,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, a.ID)
	return nil
}

func (r *addonRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "delete", map[string]interface{}{
		"addon_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("deleting addon",
		"addon_id", id,
	)

	_, err := client.Addon.
		Update().
		Where(
			addon.ID(id),
			addon.TenantID(types.GetTenantID(ctx)),
			addon.EnvironmentID(types.GetEnvironmentID(ctx)),
			addon.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Addon not found").
				WithReportableDetails(map[string]interface{}{
					"addon_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete addon").
			WithReportableDetails(map[string]interface{}{
				"addon_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, id)
	return nil
}

func (r *addonRepository) List(ctx context.Context, filter *types.AddonFilter) ([]*domainAddon.Addon, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("listing addons",
		"filter", filter,
	)

	query := client.Addon.Query()

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	entAddons, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list addons").
			WithReportableDetails(map[string]interface{}{
				"cause": err.Error(),
			}).
			Mark(ierr.ErrDatabase)
	}

	domainAddon := &domainAddon.Addon{}
	return domainAddon.FromEntList(entAddons), nil
}

func (r *addonRepository) Count(ctx context.Context, filter *types.AddonFilter) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "addon", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	query := client.Addon.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count addons").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

// Subscription Addon methods

// func (r *addonRepository) CreateSubscriptionAddon(ctx context.Context, sa *domainAddon.SubscriptionAddon) error {
// 	client := r.client.Querier(ctx)

// 	r.log.Debugw("creating subscription addon",
// 		"subscription_addon_id", sa.ID,
// 		"subscription_id", sa.SubscriptionID,
// 		"addon_id", sa.AddonID,
// 		"price_id", sa.PriceID,
// 	)

// 	// Start a span for this repository operation
// 	span := StartRepositorySpan(ctx, "subscription_addon", "create", map[string]interface{}{
// 		"subscription_addon_id": sa.ID,
// 		"subscription_id":       sa.SubscriptionID,
// 		"addon_id":              sa.AddonID,
// 	})
// 	defer FinishSpan(span)

// 	// Set environment ID from context if not already set
// 	if sa.EnvironmentID == "" {
// 		sa.EnvironmentID = types.GetEnvironmentID(ctx)
// 	}

// 	subAddonBuilder := client.SubscriptionAddon.Create().
// 		SetID(sa.ID).
// 		SetTenantID(sa.TenantID).
// 		SetSubscriptionID(sa.SubscriptionID).
// 		SetAddonID(sa.AddonID).
// 		SetPriceID(sa.PriceID).
// 		SetQuantity(sa.Quantity).
// 		SetAddonStatus(string(sa.AddonStatus)).
// 		SetProrationBehavior(string(sa.ProrationBehavior)).
// 		SetStatus(string(sa.Status)).
// 		SetCreatedAt(time.Now()).
// 		SetCreatedBy(types.GetUserID(ctx)).
// 		SetUpdatedAt(time.Now()).
// 		SetUpdatedBy(types.GetUserID(ctx)).
// 		SetEnvironmentID(sa.EnvironmentID).
// 		SetMetadata(sa.Metadata)

// 	if sa.StartDate != nil {
// 		subAddonBuilder.SetStartDate(*sa.StartDate)
// 	}

// 	if sa.EndDate != nil {
// 		subAddonBuilder.SetEndDate(*sa.EndDate)
// 	}

// 	if sa.CancellationReason != "" {
// 		subAddonBuilder.SetCancellationReason(sa.CancellationReason)
// 	}

// 	if sa.CancelledAt != nil {
// 		subAddonBuilder.SetCancelledAt(*sa.CancelledAt)
// 	}

// 	if sa.ProratedAmount != nil {
// 		subAddonBuilder.SetProratedAmount(*sa.ProratedAmount)
// 	}

// 	if sa.UsageLimit != nil {
// 		subAddonBuilder.SetUsageLimit(*sa.UsageLimit)
// 	}

// 	if sa.UsageResetPeriod != "" {
// 		subAddonBuilder.SetUsageResetPeriod(sa.UsageResetPeriod)
// 	}

// 	if sa.UsageResetDate != nil {
// 		subAddonBuilder.SetUsageResetDate(*sa.UsageResetDate)
// 	}

// 	if sa.CreatedBy != "" {
// 		subAddonBuilder.SetCreatedBy(sa.CreatedBy)
// 	}

// 	if sa.UpdatedBy != "" {
// 		subAddonBuilder.SetUpdatedBy(sa.UpdatedBy)
// 	}

// 	if sa.Metadata != nil {
// 		subAddonBuilder.SetMetadata(sa.Metadata)
// 	}

// 	_, err := subAddonBuilder.Save(ctx)
// 	if err != nil {
// 		SetSpanError(span, err)
// 		return ierr.WithError(err).
// 			WithHint("Failed to create subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": sa.ID,
// 				"subscription_id":       sa.SubscriptionID,
// 				"addon_id":              sa.AddonID,
// 				"price_id":              sa.PriceID,
// 			}).
// 			Mark(ierr.ErrDatabase)
// 	}

// 	return nil
// }

// func (r *addonRepository) GetSubscriptionAddonByID(ctx context.Context, id string) (*domainAddon.SubscriptionAddon, error) {
// 	// Start a span for this repository operation
// 	span := StartRepositorySpan(ctx, "subscription_addon", "get_by_id", map[string]interface{}{
// 		"subscription_addon_id": id,
// 	})
// 	defer FinishSpan(span)

// 	client := r.client.Querier(ctx)

// 	r.log.Debugw("getting subscription addon by id",
// 		"subscription_addon_id", id,
// 	)

// 	entSubAddon, err := client.SubscriptionAddon.
// 		Query().
// 		Where(
// 			subscriptionaddon.ID(id),
// 			subscriptionaddon.TenantID(types.GetTenantID(ctx)),
// 			subscriptionaddon.EnvironmentID(types.GetEnvironmentID(ctx)),
// 		).
// 		First(ctx)

// 	if err != nil {
// 		SetSpanError(span, err)
// 		if ent.IsNotFound(err) {
// 			return nil, ierr.WithError(err).
// 				WithHint("Subscription addon not found").
// 				WithReportableDetails(map[string]interface{}{
// 					"subscription_addon_id": id,
// 				}).
// 				Mark(ierr.ErrNotFound)
// 		}
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to get subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": id,
// 			}).
// 			Mark(ierr.ErrDatabase)
// 	}

// 	domainSubAddon := &domainAddon.SubscriptionAddon{}
// 	return domainSubAddon.FromEnt(entSubAddon), nil
// }

// func (r *addonRepository) GetSubscriptionAddons(ctx context.Context, subscriptionID string) ([]*domainAddon.SubscriptionAddon, error) {
// 	// Start a span for this repository operation
// 	span := StartRepositorySpan(ctx, "subscription_addon", "get_by_subscription", map[string]interface{}{
// 		"subscription_id": subscriptionID,
// 	})
// 	defer FinishSpan(span)

// 	client := r.client.Querier(ctx)

// 	r.log.Debugw("getting subscription addons",
// 		"subscription_id", subscriptionID,
// 	)

// 	entSubAddons, err := client.SubscriptionAddon.
// 		Query().
// 		Where(
// 			subscriptionaddon.SubscriptionID(subscriptionID),
// 			subscriptionaddon.TenantID(types.GetTenantID(ctx)),
// 			subscriptionaddon.EnvironmentID(types.GetEnvironmentID(ctx)),
// 			subscriptionaddon.StatusEQ(string(types.StatusPublished)),
// 		).
// 		All(ctx)

// 	if err != nil {
// 		SetSpanError(span, err)
// 		return nil, ierr.WithError(err).
// 			WithHint("Failed to get subscription addons").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_id": subscriptionID,
// 			}).
// 			Mark(ierr.ErrDatabase)
// 	}

// 	domainSubAddons := make([]*domainAddon.SubscriptionAddon, len(entSubAddons))
// 	for i, entSubAddon := range entSubAddons {
// 		domainSubAddon := &domainAddon.SubscriptionAddon{}
// 		domainSubAddons[i] = domainSubAddon.FromEnt(entSubAddon)
// 	}

// 	return domainSubAddons, nil
// }

// func (r *addonRepository) UpdateSubscriptionAddon(ctx context.Context, sa *domainAddon.SubscriptionAddon) error {
// 	// Start a span for this repository operation
// 	span := StartRepositorySpan(ctx, "subscription_addon", "update", map[string]interface{}{
// 		"subscription_addon_id": sa.ID,
// 		"addon_status":          sa.AddonStatus,
// 	})
// 	defer FinishSpan(span)

// 	client := r.client.Querier(ctx)

// 	r.log.Debugw("updating subscription addon",
// 		"subscription_addon_id", sa.ID,
// 		"addon_status", sa.AddonStatus,
// 	)

// 	updateBuilder := client.SubscriptionAddon.
// 		UpdateOneID(sa.ID).
// 		Where(
// 			subscriptionaddon.TenantID(types.GetTenantID(ctx)),
// 			subscriptionaddon.EnvironmentID(types.GetEnvironmentID(ctx)),
// 		).
// 		SetQuantity(sa.Quantity).
// 		SetAddonStatus(string(sa.AddonStatus)).
// 		SetProrationBehavior(string(sa.ProrationBehavior)).
// 		SetUpdatedAt(time.Now()).
// 		SetUpdatedBy(types.GetUserID(ctx))

// 	if sa.EndDate != nil {
// 		updateBuilder.SetEndDate(*sa.EndDate)
// 	}

// 	if sa.CancellationReason != "" {
// 		updateBuilder.SetCancellationReason(sa.CancellationReason)
// 	}

// 	if sa.CancelledAt != nil {
// 		updateBuilder.SetCancelledAt(*sa.CancelledAt)
// 	}

// 	if sa.ProratedAmount != nil {
// 		updateBuilder.SetProratedAmount(*sa.ProratedAmount)
// 	}

// 	if sa.Metadata != nil {
// 		updateBuilder.SetMetadata(sa.Metadata)
// 	}

// 	_, err := updateBuilder.Save(ctx)
// 	if err != nil {
// 		SetSpanError(span, err)
// 		if ent.IsNotFound(err) {
// 			return ierr.WithError(err).
// 				WithHint("Subscription addon not found").
// 				WithReportableDetails(map[string]interface{}{
// 					"subscription_addon_id": sa.ID,
// 				}).
// 				Mark(ierr.ErrNotFound)
// 		}
// 		return ierr.WithError(err).
// 			WithHint("Failed to update subscription addon").
// 			WithReportableDetails(map[string]interface{}{
// 				"subscription_addon_id": sa.ID,
// 			}).
// 			Mark(ierr.ErrDatabase)
// 	}

// 	return nil
// }

// AddonQuery type alias for better readability
type AddonQuery = *ent.AddonQuery

// AddonQueryOptions implements BaseQueryOptions for addon queries
type AddonQueryOptions struct{}

func (o AddonQueryOptions) ApplyTenantFilter(ctx context.Context, query AddonQuery) AddonQuery {
	return query.Where(addon.TenantID(types.GetTenantID(ctx)))
}

func (o AddonQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query AddonQuery) AddonQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(addon.EnvironmentID(environmentID))
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

func (o AddonQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in addon query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
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
	default:
		return field
	}
}

func (o AddonQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.AddonFilter, query AddonQuery) AddonQuery {
	if f == nil {
		return query
	}

	// Apply entity-specific filters
	if len(f.AddonIDs) > 0 {
		query = query.Where(addon.IDIn(f.AddonIDs...))
	}

	if f.AddonType != "" {
		query = query.Where(addon.Type(string(f.AddonType)))
	}

	if len(f.LookupKeys) > 0 {
		query = query.Where(addon.LookupKeyIn(f.LookupKeys...))
	}

	// Apply DSL filters if provided
	if len(f.Filters) > 0 {
		query, _ = dsl.ApplyFilters[AddonQuery, predicate.Addon](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Addon { return predicate.Addon(p) },
		)
	}

	// Apply time range filters
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(addon.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(addon.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
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

	r.log.Debugw("set addon in cache", "id", addon.ID, "cache_key", cacheKey)
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

func (r *addonRepository) DeleteCache(ctx context.Context, key string) {
	span := cache.StartCacheSpan(ctx, "addon", "delete", map[string]interface{}{
		"addon_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixAddon, tenantID, environmentID, key)
	r.cache.Delete(ctx, cacheKey)
}
