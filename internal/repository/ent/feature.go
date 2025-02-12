package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/feature"
	domainFeature "github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type featureRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts FeatureQueryOptions
}

func NewFeatureRepository(client postgres.IClient, log *logger.Logger) domainFeature.Repository {
	return &featureRepository{
		client:    client,
		log:       log,
		queryOpts: FeatureQueryOptions{},
	}
}

func (r *featureRepository) Create(ctx context.Context, f *domainFeature.Feature) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating feature",
		"feature_id", f.ID,
		"tenant_id", f.TenantID,
		"name", f.Name,
		"lookup_key", f.LookupKey,
	)

	feature, err := client.Feature.Create().
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
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create feature: %w", err)
	}

	*f = *domainFeature.FromEnt(feature)
	return nil
}

func (r *featureRepository) Get(ctx context.Context, id string) (*domainFeature.Feature, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting feature",
		"feature_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	f, err := client.Feature.Query().
		Where(
			feature.ID(id),
			feature.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("feature not found")
		}
		return nil, fmt.Errorf("failed to get feature: %w", err)
	}

	return domainFeature.FromEnt(f), nil
}

func (r *featureRepository) List(ctx context.Context, filter *types.FeatureFilter) ([]*domainFeature.Feature, error) {
	if filter == nil {
		filter = &types.FeatureFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Querier(ctx)
	query := client.Feature.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	features, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list features: %w", err)
	}

	return domainFeature.FromEntList(features), nil
}

func (r *featureRepository) Count(ctx context.Context, filter *types.FeatureFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Feature.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count features: %w", err)
	}

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
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	return r.List(ctx, filter)
}

func (r *featureRepository) Update(ctx context.Context, f *domainFeature.Feature) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating feature",
		"feature_id", f.ID,
		"tenant_id", f.TenantID,
	)

	_, err := client.Feature.Update().
		Where(
			feature.ID(f.ID),
			feature.TenantID(f.TenantID),
		).
		SetName(f.Name).
		SetDescription(f.Description).
		SetStatus(string(f.Status)).
		SetUnitSingular(f.UnitSingular).
		SetUnitPlural(f.UnitPlural).
		SetMetadata(map[string]string(f.Metadata)).
		SetMeterID(f.MeterID).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("feature not found")
		}
		return fmt.Errorf("failed to update feature: %w", err)
	}

	return nil
}

func (r *featureRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting feature",
		"feature_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Feature.Update().
		Where(
			feature.ID(id),
			feature.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("feature not found")
		}
		return fmt.Errorf("failed to delete feature: %w", err)
	}

	return nil
}

// FeatureQuery type alias for better readability
type FeatureQuery = *ent.FeatureQuery

// FeatureQueryOptions implements BaseQueryOptions for feature queries
type FeatureQueryOptions struct{}

func (o FeatureQueryOptions) ApplyTenantFilter(ctx context.Context, query FeatureQuery) FeatureQuery {
	return query.Where(feature.TenantID(types.GetTenantID(ctx)))
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
	default:
		return field
	}
}

func (o FeatureQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.FeatureFilter, query FeatureQuery) FeatureQuery {
	if f == nil {
		return query
	}

	// Apply feature IDs filter if specified
	if len(f.FeatureIDs) > 0 {
		query = query.Where(feature.IDIn(f.FeatureIDs...))
	}

	// Apply key filter if specified
	if f.LookupKey != "" {
		query = query.Where(feature.LookupKey(f.LookupKey))
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

	return query
}
