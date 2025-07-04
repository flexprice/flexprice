package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	entTaxConfig "github.com/flexprice/flexprice/ent/taxassociation"
	"github.com/flexprice/flexprice/internal/cache"
	domainTaxConfig "github.com/flexprice/flexprice/internal/domain/taxassociation"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type taxConfigRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts TaxConfigQueryOptions
	cache     cache.Cache
}

// NewTaxConfigRepository creates a new tax config repository
func NewTaxConfigRepository(client postgres.IClient, logger *logger.Logger, cache cache.Cache) domainTaxConfig.Repository {
	return &taxConfigRepository{
		client:    client,
		logger:    logger,
		queryOpts: TaxConfigQueryOptions{},
		cache:     cache,
	}
}

type TaxConfigQuery = *ent.TaxAssociationQuery

type TaxConfigQueryOptions struct{}

func (o TaxConfigQueryOptions) ApplyTenantFilter(ctx context.Context, query TaxConfigQuery) TaxConfigQuery {
	return query.Where(entTaxConfig.TenantID(types.GetTenantID(ctx)))
}

func (o TaxConfigQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query TaxConfigQuery) TaxConfigQuery {
	envID := types.GetEnvironmentID(ctx)
	if envID != "" {
		return query.Where(entTaxConfig.EnvironmentID(envID))
	}
	return query
}

func (o TaxConfigQueryOptions) ApplyStatusFilter(query TaxConfigQuery, status string) TaxConfigQuery {
	if status == "" {
		return query.Where(entTaxConfig.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(entTaxConfig.Status(status))
}

func (o TaxConfigQueryOptions) ApplySortFilter(query TaxConfigQuery, field, order string) TaxConfigQuery {
	if field != "" {
		if order == types.OrderDesc {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o TaxConfigQueryOptions) ApplyPaginationFilter(query TaxConfigQuery, limit, offset int) TaxConfigQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o TaxConfigQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return entTaxConfig.FieldCreatedAt
	case "updated_at":
		return entTaxConfig.FieldUpdatedAt
	case "priority":
		return entTaxConfig.FieldPriority
	case "auto_apply":
		return entTaxConfig.FieldAutoApply
	case "currency":
		return entTaxConfig.FieldCurrency
	case "metadata":
		return entTaxConfig.FieldMetadata
	case "environment_id":
		return entTaxConfig.FieldEnvironmentID
	case "tax_rate_id":
		return entTaxConfig.FieldTaxRateID
	case "entity_type":
		return entTaxConfig.FieldEntityType
	case "entity_id":
		return entTaxConfig.FieldEntityID
	case "tenant_id":
		return entTaxConfig.FieldTenantID
	case "id":
		return entTaxConfig.FieldID
	default:
		return ""
	}
}

func (o TaxConfigQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.TaxConfigFilter, query TaxConfigQuery) (TaxConfigQuery, error) {
	if f == nil {
		return query, nil
	}
	if len(f.TaxConfigIDs) > 0 {
		query = query.Where(entTaxConfig.IDIn(f.TaxConfigIDs...))
	}
	if len(f.TaxRateIDs) > 0 {
		query = query.Where(entTaxConfig.TaxRateIDIn(f.TaxRateIDs...))
	}
	if f.EntityType != "" {
		query = query.Where(entTaxConfig.EntityType(f.EntityType))
	}
	if f.EntityID != "" {
		query = query.Where(entTaxConfig.EntityID(f.EntityID))
	}
	if f.Currency != "" {
		query = query.Where(entTaxConfig.Currency(f.Currency))
	}
	if f.AutoApply != nil {
		query = query.Where(entTaxConfig.AutoApply(lo.FromPtr(f.AutoApply)))
	}
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(entTaxConfig.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(entTaxConfig.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}
	return query, nil
}

// Create creates a new tax config
func (r *taxConfigRepository) Create(ctx context.Context, t *domainTaxConfig.TaxConfig) error {
	client := r.client.Querier(ctx)
	r.logger.Debugw("creating tax config", "tax_config_id", t.ID, "tax_rate_id", t.TaxRateID, "entity_type", t.EntityType, "entity_id", t.EntityID)

	span := StartRepositorySpan(ctx, "taxconfig", "create", map[string]interface{}{
		"tax_config_id": t.ID,
		"tax_rate_id":   t.TaxRateID,
		"entity_type":   t.EntityType,
		"entity_id":     t.EntityID,
	})
	defer FinishSpan(span)

	if t.EnvironmentID == "" {
		t.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.TaxAssociation.Create().
		SetID(t.ID).
		SetTaxRateID(t.TaxRateID).
		SetEntityType(t.EntityType).
		SetCurrency(t.Currency).
		SetPriority(t.Priority).
		SetAutoApply(t.AutoApply).
		SetMetadata(t.Metadata).
		SetEnvironmentID(t.EnvironmentID).
		SetEntityID(t.EntityID).
		SetCreatedAt(t.CreatedAt).
		SetUpdatedAt(t.UpdatedAt).
		SetCreatedBy(t.CreatedBy).
		SetTenantID(t.TenantID).
		SetUpdatedBy(t.UpdatedBy).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create tax config").
			WithReportableDetails(map[string]interface{}{
				"tax_config_id": t.ID,
				"tax_rate_id":   t.TaxRateID,
				"entity_type":   t.EntityType,
				"entity_id":     t.EntityID,
			}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

// Get retrieves a tax config by ID
func (r *taxConfigRepository) Get(ctx context.Context, id string) (*domainTaxConfig.TaxConfig, error) {
	span := StartRepositorySpan(ctx, "taxconfig", "get", map[string]interface{}{
		"tax_config_id": id,
	})
	defer FinishSpan(span)

	if cached := r.GetCache(ctx, id); cached != nil {
		return cached, nil
	}

	client := r.client.Querier(ctx)
	r.logger.Debugw("getting tax config", "tax_config_id", id)

	tc, err := client.TaxAssociation.Query().
		Where(
			entTaxConfig.ID(id),
			entTaxConfig.TenantID(types.GetTenantID(ctx)),
			entTaxConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("TaxConfig with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"tax_config_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get tax config").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	taxConfig := domainTaxConfig.FromEnt(tc)
	r.SetCache(ctx, taxConfig)
	return taxConfig, nil
}

// Update updates a tax config
func (r *taxConfigRepository) Update(ctx context.Context, t *domainTaxConfig.TaxConfig) error {
	client := r.client.Querier(ctx)
	r.logger.Debugw("updating tax config", "tax_config_id", t.ID)

	span := StartRepositorySpan(ctx, "taxconfig", "update", map[string]interface{}{
		"tax_config_id": t.ID,
	})
	defer FinishSpan(span)

	_, err := client.TaxAssociation.Update().
		Where(
			entTaxConfig.ID(t.ID),
			entTaxConfig.TenantID(types.GetTenantID(ctx)),
			entTaxConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetEntityID(t.EntityID).
		SetPriority(t.Priority).
		SetAutoApply(t.AutoApply).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetMetadata(t.Metadata).
		Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("TaxConfig with ID %s was not found", t.ID).
				WithReportableDetails(map[string]any{
					"tax_config_id": t.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update tax config").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	r.DeleteCache(ctx, t)
	return nil
}

// Delete deletes a tax config by ID
func (r *taxConfigRepository) Delete(ctx context.Context, t *domainTaxConfig.TaxConfig) error {
	client := r.client.Querier(ctx)
	r.logger.Debugw("deleting tax config", "tax_config_id", t.ID)

	span := StartRepositorySpan(ctx, "taxconfig", "delete", map[string]interface{}{
		"tax_config_id": t.ID,
	})
	defer FinishSpan(span)

	_, err := client.TaxAssociation.Update().
		Where(
			entTaxConfig.ID(t.ID),
			entTaxConfig.TenantID(types.GetTenantID(ctx)),
			entTaxConfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("TaxConfig with ID %s was not found", t.ID).
				WithReportableDetails(map[string]any{
					"tax_config_id": t.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete tax config").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	r.DeleteCache(ctx, t)
	return nil
}

// List retrieves tax configs based on filter
func (r *taxConfigRepository) List(ctx context.Context, filter *types.TaxConfigFilter) ([]*domainTaxConfig.TaxConfig, error) {
	client := r.client.Querier(ctx)

	span := StartRepositorySpan(ctx, "taxconfig", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.TaxAssociation.Query()
	query = r.queryOpts.ApplyTenantFilter(ctx, query)
	query = r.queryOpts.ApplyEnvironmentFilter(ctx, query)
	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax configs").
			Mark(ierr.ErrDatabase)
	}
	if filter != nil {
		query = r.queryOpts.ApplySortFilter(query, filter.GetSort(), filter.GetOrder())
		query = r.queryOpts.ApplyPaginationFilter(query, filter.GetLimit(), filter.GetOffset())
	}
	taxconfigs, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax configs").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainTaxConfig.FromEntList(taxconfigs), nil
}

// Count counts tax configs based on filter
func (r *taxConfigRepository) Count(ctx context.Context, filter *types.TaxConfigFilter) (int, error) {
	client := r.client.Querier(ctx)

	span := StartRepositorySpan(ctx, "taxconfig", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.TaxAssociation.Query()
	query = r.queryOpts.ApplyTenantFilter(ctx, query)
	query = r.queryOpts.ApplyEnvironmentFilter(ctx, query)
	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count tax configs").
			Mark(ierr.ErrDatabase)
	}
	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count tax configs").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return count, nil
}

// Cache operations
func (r *taxConfigRepository) SetCache(ctx context.Context, t *domainTaxConfig.TaxConfig) {
	span := cache.StartCacheSpan(ctx, "taxconfig", "set", map[string]interface{}{
		"tax_config_id": t.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixTaxAssociation, tenantID, environmentID, t.ID)
	r.cache.Set(ctx, cacheKey, t, cache.ExpiryDefaultInMemory)
}

func (r *taxConfigRepository) GetCache(ctx context.Context, key string) *domainTaxConfig.TaxConfig {
	span := cache.StartCacheSpan(ctx, "taxconfig", "get", map[string]interface{}{
		"tax_config_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixTaxAssociation, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if tc, ok := value.(*domainTaxConfig.TaxConfig); ok {
			return tc
		}
	}
	return nil
}

func (r *taxConfigRepository) DeleteCache(ctx context.Context, t *domainTaxConfig.TaxConfig) {
	span := cache.StartCacheSpan(ctx, "taxconfig", "delete", map[string]interface{}{
		"tax_config_id": t.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixTaxAssociation, tenantID, environmentID, t.ID)
	r.cache.Delete(ctx, cacheKey)
}
