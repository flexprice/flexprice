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

type taxAssociationRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts TaxAssociationQueryOptions
	cache     cache.Cache
}

// NewTaxAssociationRepository creates a new tax association repository
func NewTaxAssociationRepository(client postgres.IClient, logger *logger.Logger, cache cache.Cache) domainTaxConfig.Repository {
	return &taxAssociationRepository{
		client:    client,
		logger:    logger,
		queryOpts: TaxAssociationQueryOptions{},
		cache:     cache,
	}
}

type TaxAssociationQuery = *ent.TaxAssociationQuery

type TaxAssociationQueryOptions struct{}

func (o TaxAssociationQueryOptions) ApplyTenantFilter(ctx context.Context, query TaxAssociationQuery) TaxAssociationQuery {
	return query.Where(entTaxConfig.TenantID(types.GetTenantID(ctx)))
}

func (o TaxAssociationQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query TaxAssociationQuery) TaxAssociationQuery {
	envID := types.GetEnvironmentID(ctx)
	if envID != "" {
		return query.Where(entTaxConfig.EnvironmentID(envID))
	}
	return query
}

func (o TaxAssociationQueryOptions) ApplyStatusFilter(query TaxAssociationQuery, status string) TaxAssociationQuery {
	if status == "" {
		return query.Where(entTaxConfig.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(entTaxConfig.Status(status))
}

func (o TaxAssociationQueryOptions) ApplySortFilter(query TaxAssociationQuery, field, order string) TaxAssociationQuery {
	if field != "" {
		if order == types.OrderDesc {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o TaxAssociationQueryOptions) ApplyPaginationFilter(query TaxAssociationQuery, limit, offset int) TaxAssociationQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o TaxAssociationQueryOptions) GetFieldName(field string) string {
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

func (o TaxAssociationQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.TaxAssociationFilter, query TaxAssociationQuery) (TaxAssociationQuery, error) {
	if f == nil {
		return query, nil
	}
	if len(f.TaxAssociationIDs) > 0 {
		query = query.Where(entTaxConfig.IDIn(f.TaxAssociationIDs...))
	}
	if len(f.TaxRateIDs) > 0 {
		query = query.Where(entTaxConfig.TaxRateIDIn(f.TaxRateIDs...))
	}
	if f.EntityType != "" {
		query = query.Where(entTaxConfig.EntityType(string(f.EntityType)))
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
func (r *taxAssociationRepository) Create(ctx context.Context, t *domainTaxConfig.TaxAssociation) error {
	client := r.client.Querier(ctx)
	r.logger.Debugw("creating tax association", "tax_association_id", t.ID, "tax_rate_id", t.TaxRateID, "entity_type", t.EntityType, "entity_id", t.EntityID)

	span := StartRepositorySpan(ctx, "taxassociation", "create", map[string]interface{}{
		"tax_association_id": t.ID,
		"tax_rate_id":        t.TaxRateID,
		"entity_type":        t.EntityType,
		"entity_id":          t.EntityID,
	})
	defer FinishSpan(span)

	if t.EnvironmentID == "" {
		t.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.TaxAssociation.Create().
		SetID(t.ID).
		SetTaxRateID(t.TaxRateID).
		SetEntityType(string(t.EntityType)).
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
			WithHint("Failed to create tax association").
			WithReportableDetails(map[string]interface{}{
				"tax_association_id": t.ID,
				"tax_rate_id":        t.TaxRateID,
				"entity_type":        t.EntityType,
				"entity_id":          t.EntityID,
			}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

// Get retrieves a tax association by ID
func (r *taxAssociationRepository) Get(ctx context.Context, id string) (*domainTaxConfig.TaxAssociation, error) {
	span := StartRepositorySpan(ctx, "taxassociation", "get", map[string]interface{}{
		"tax_association_id": id,
	})
	defer FinishSpan(span)

	if cached := r.GetCache(ctx, id); cached != nil {
		return cached, nil
	}

	client := r.client.Querier(ctx)
	r.logger.Debugw("getting tax association", "tax_association_id", id)

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
				WithHintf("TaxAssociation with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"tax_association_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get tax association").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	taxAssociation := domainTaxConfig.FromEnt(tc)
	r.SetCache(ctx, taxAssociation)
	return taxAssociation, nil
}

// Update updates a tax association
func (r *taxAssociationRepository) Update(ctx context.Context, t *domainTaxConfig.TaxAssociation) error {
	client := r.client.Querier(ctx)
	r.logger.Debugw("updating tax association", "tax_association_id", t.ID)

	span := StartRepositorySpan(ctx, "taxassociation", "update", map[string]interface{}{
		"tax_association_id": t.ID,
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
				WithHintf("TaxAssociation with ID %s was not found", t.ID).
				WithReportableDetails(map[string]any{
					"tax_association_id": t.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update tax association").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	r.DeleteCache(ctx, t)
	return nil
}

// Delete deletes a tax association by ID
func (r *taxAssociationRepository) Delete(ctx context.Context, t *domainTaxConfig.TaxAssociation) error {
	client := r.client.Querier(ctx)
	r.logger.Debugw("deleting tax association", "tax_association_id", t.ID)

	span := StartRepositorySpan(ctx, "taxassociation", "delete", map[string]interface{}{
		"tax_association_id": t.ID,
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
				WithHintf("TaxAssociation with ID %s was not found", t.ID).
				WithReportableDetails(map[string]any{
					"tax_association_id": t.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete tax association").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	r.DeleteCache(ctx, t)
	return nil
}

// List retrieves tax configs based on filter
func (r *taxAssociationRepository) List(ctx context.Context, filter *types.TaxAssociationFilter) ([]*domainTaxConfig.TaxAssociation, error) {
	client := r.client.Querier(ctx)

	span := StartRepositorySpan(ctx, "taxassociation", "list", map[string]interface{}{
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
			WithHint("Failed to list tax associations").
			Mark(ierr.ErrDatabase)
	}
	if filter != nil {
		query = r.queryOpts.ApplySortFilter(query, filter.GetSort(), filter.GetOrder())
		query = r.queryOpts.ApplyPaginationFilter(query, filter.GetLimit(), filter.GetOffset())
	}
	taxassociations, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax associations").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainTaxConfig.FromEntList(taxassociations), nil
}

// Count counts tax configs based on filter
func (r *taxAssociationRepository) Count(ctx context.Context, filter *types.TaxAssociationFilter) (int, error) {
	client := r.client.Querier(ctx)

	span := StartRepositorySpan(ctx, "taxassociation", "count", map[string]interface{}{
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
			WithHint("Failed to count tax associations").
			Mark(ierr.ErrDatabase)
	}
	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count tax associations").
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return count, nil
}

// Cache operations
func (r *taxAssociationRepository) SetCache(ctx context.Context, t *domainTaxConfig.TaxAssociation) {
	span := cache.StartCacheSpan(ctx, "taxassociation", "set", map[string]interface{}{
		"tax_association_id": t.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixTaxAssociation, tenantID, environmentID, t.ID)
	r.cache.Set(ctx, cacheKey, t, cache.ExpiryDefaultInMemory)
}

func (r *taxAssociationRepository) GetCache(ctx context.Context, key string) *domainTaxConfig.TaxAssociation {
	span := cache.StartCacheSpan(ctx, "taxassociation", "get", map[string]interface{}{
		"tax_association_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixTaxAssociation, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if tc, ok := value.(*domainTaxConfig.TaxAssociation); ok {
			return tc
		}
	}
	return nil
}

func (r *taxAssociationRepository) DeleteCache(ctx context.Context, t *domainTaxConfig.TaxAssociation) {
	span := cache.StartCacheSpan(ctx, "taxassociation", "delete", map[string]interface{}{
		"tax_association_id": t.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixTaxAssociation, tenantID, environmentID, t.ID)
	r.cache.Delete(ctx, cacheKey)
}
