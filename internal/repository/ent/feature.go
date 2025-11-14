package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/feature"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domainFeature "github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type featureRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts FeatureQueryOptions
	cache     cache.Cache
}

func NewFeatureRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainFeature.Repository {
	return &featureRepository{
		client:    client,
		log:       log,
		queryOpts: FeatureQueryOptions{},
		cache:     cache,
	}
}

func (r *featureRepository) Create(ctx context.Context, f *domainFeature.Feature) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating feature",
		"feature_id", f.ID,
		"tenant_id", f.TenantID,
		"name", f.Name,
		"lookup_key", f.LookupKey,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "feature", "create", map[string]interface{}{
		"feature_id": f.ID,
		"name":       f.Name,
		"lookup_key": f.LookupKey,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if f.EnvironmentID == "" {
		f.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	createQuery := client.Feature.Create().
		SetID(f.ID).
		SetName(f.Name).
		SetType(string(f.Type)).
		SetDescription(f.Description).
		SetLookupKey(f.LookupKey).
		SetStatus(string(f.Status)).
		SetUnitSingular(f.UnitSingular).
		SetUnitPlural(f.UnitPlural).
		SetMetadata(map[string]string(f.Metadata)).
		SetMeterID(f.MeterID).
		SetTenantID(f.TenantID).
		SetCreatedAt(f.CreatedAt).
		SetUpdatedAt(f.UpdatedAt).
		SetCreatedBy(f.CreatedBy).
		SetUpdatedBy(f.UpdatedBy).
		SetEnvironmentID(f.EnvironmentID)

	// Set alert settings if provided
	if f.AlertSettings != nil {
		createQuery = createQuery.SetAlertSettings(*f.AlertSettings)
	}

	feature, err := createQuery.Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A feature with this lookup key already exists").
				WithReportableDetails(map[string]any{
					"lookup_key": f.LookupKey,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create feature").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*f = *domainFeature.FromEnt(feature)
	return nil
}

func (r *featureRepository) Get(ctx context.Context, id string) (*domainFeature.Feature, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "feature", "get", map[string]interface{}{
		"feature_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedFeature := r.GetCache(ctx, id); cachedFeature != nil {
		return cachedFeature, nil
	}

	client := r.client.Reader(ctx)

	r.log.Debugw("getting feature",
		"feature_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	f, err := client.Feature.Query().
		Where(
			feature.ID(id),
			feature.TenantID(types.GetTenantID(ctx)),
			feature.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Feature with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"feature_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get feature with ID %s", id).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	featureData := domainFeature.FromEnt(f)
	r.SetCache(ctx, featureData)
	return featureData, nil
}

func (r *featureRepository) List(ctx context.Context, filter *types.FeatureFilter) ([]*domainFeature.Feature, error) {
	if filter == nil {
		filter = &types.FeatureFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "feature", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.Feature.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return nil, err
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	features, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list features").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainFeature.FromEntList(features), nil
}

func (r *featureRepository) Count(ctx context.Context, filter *types.FeatureFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "feature", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Feature.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count features").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *featureRepository) ListAll(ctx context.Context, filter *types.FeatureFilter) ([]*domainFeature.Feature, error) {
	if filter == nil {
		filter = types.NewNoLimitFeatureFilter()
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

func (r *featureRepository) Update(ctx context.Context, f *domainFeature.Feature) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating feature",
		"feature_id", f.ID,
		"tenant_id", f.TenantID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "feature", "update", map[string]interface{}{
		"feature_id": f.ID,
	})
	defer FinishSpan(span)

	updateQuery := client.Feature.Update().
		Where(
			feature.ID(f.ID),
			feature.TenantID(f.TenantID),
			feature.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(f.Name).
		SetDescription(f.Description).
		SetStatus(string(f.Status)).
		SetUnitSingular(f.UnitSingular).
		SetUnitPlural(f.UnitPlural).
		SetMetadata(map[string]string(f.Metadata)).
		SetMeterID(f.MeterID).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx))

	// Set alert settings if provided
	if f.AlertSettings != nil {
		updateQuery = updateQuery.SetAlertSettings(*f.AlertSettings)
	}

	_, err := updateQuery.Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Feature with ID %s was not found", f.ID).
				WithReportableDetails(map[string]any{
					"feature_id": f.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update feature").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, f.ID)
	return nil
}

func (r *featureRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("deleting feature",
		"feature_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "feature", "delete", map[string]interface{}{
		"feature_id": id,
	})
	defer FinishSpan(span)

	_, err := client.Feature.Update().
		Where(
			feature.ID(id),
			feature.TenantID(types.GetTenantID(ctx)),
			feature.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Feature with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"feature_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete feature").
			WithReportableDetails(map[string]any{
				"feature_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, id)
	return nil
}

// ListByIDs retrieves features by their IDs
func (r *featureRepository) ListByIDs(ctx context.Context, featureIDs []string) ([]*domainFeature.Feature, error) {
	if len(featureIDs) == 0 {
		return []*domainFeature.Feature{}, nil
	}

	r.log.Debugw("listing features by IDs", "feature_ids", featureIDs)

	// Create a filter with feature IDs
	filter := &types.FeatureFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		FeatureIDs:  featureIDs,
	}

	// Use the existing List method
	return r.List(ctx, filter)
}

// FeatureQuery type alias for better readability
type FeatureQuery = *ent.FeatureQuery

// FeatureQueryOptions implements query options for feature filtering and sorting
type FeatureQueryOptions struct{}

func (o FeatureQueryOptions) ApplyTenantFilter(ctx context.Context, query FeatureQuery) FeatureQuery {
	return query.Where(feature.TenantID(types.GetTenantID(ctx)))
}

func (o FeatureQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query FeatureQuery) FeatureQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(feature.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o FeatureQueryOptions) ApplyStatusFilter(query FeatureQuery, status string) FeatureQuery {
	if status == "" {
		return query.Where(feature.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(feature.Status(status))
}

func (o FeatureQueryOptions) ApplySortFilter(query FeatureQuery, field string, order string) FeatureQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o FeatureQueryOptions) ApplyPaginationFilter(query FeatureQuery, limit int, offset int) FeatureQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o FeatureQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return feature.FieldCreatedAt
	case "updated_at":
		return feature.FieldUpdatedAt
	case "name":
		return feature.FieldName
	case "lookup_key":
		return feature.FieldLookupKey
	case "type":
		return feature.FieldType
	case "status":
		return feature.FieldStatus
	default:
		// unknown field
		return ""
	}
}

func (o FeatureQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.FeatureFilter, query FeatureQuery) (FeatureQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply feature IDs filter if specified
	if len(f.FeatureIDs) > 0 {
		query = query.Where(feature.IDIn(f.FeatureIDs...))
	}

	// Apply meter IDs filter if specified
	if len(f.MeterIDs) > 0 {
		query = query.Where(feature.MeterIDIn(f.MeterIDs...))
	}

	// Apply key filter if specified
	if f.LookupKey != "" {
		query = query.Where(feature.LookupKey(f.LookupKey))
	}

	// Apply lookup keys filter if specified
	if len(f.LookupKeys) > 0 {
		query = query.Where(feature.LookupKeyIn(f.LookupKeys...))
	}

	if f.NameContains != "" {
		query = query.Where(feature.NameContainsFold(f.NameContains))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(feature.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(feature.CreatedAtLTE(*f.EndTime))
		}
	}

	// Apply filters using the generic function
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[FeatureQuery, predicate.Feature](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Feature { return predicate.Feature(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[FeatureQuery, feature.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) feature.OrderOption { return feature.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (o FeatureQueryOptions) GetFieldResolver(st string) (string, error) {
	fieldName := o.GetFieldName(st)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in feature query", st).
			WithHintf("Unknown field name '%s' in feature query", st).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (r *featureRepository) SetCache(ctx context.Context, feature *domainFeature.Feature) {
	span := cache.StartCacheSpan(ctx, "feature", "set", map[string]interface{}{
		"feature_id": feature.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixFeature, tenantID, environmentID, feature.ID)
	r.cache.Set(ctx, cacheKey, feature, cache.ExpiryDefaultInMemory)
}

func (r *featureRepository) GetCache(ctx context.Context, key string) *domainFeature.Feature {
	span := cache.StartCacheSpan(ctx, "feature", "get", map[string]interface{}{
		"feature_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixFeature, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainFeature.Feature)
	}
	return nil
}

func (r *featureRepository) DeleteCache(ctx context.Context, featureID string) {
	span := cache.StartCacheSpan(ctx, "feature", "delete", map[string]interface{}{
		"feature_id": featureID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixFeature, tenantID, environmentID, featureID)
	r.cache.Delete(ctx, cacheKey)
}
