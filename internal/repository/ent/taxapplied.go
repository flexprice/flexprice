package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/taxapplied"
	"github.com/flexprice/flexprice/internal/cache"
	domainTaxApplied "github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type taxappliedRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts TaxAppliedQueryOptions
	cache     cache.Cache
}

func NewTaxAppliedRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainTaxApplied.Repository {
	return &taxappliedRepository{
		client:    client,
		log:       log,
		queryOpts: TaxAppliedQueryOptions{},
		cache:     cache,
	}
}

func (r *taxappliedRepository) Create(ctx context.Context, ta *domainTaxApplied.TaxApplied) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxapplied", "create", map[string]interface{}{
		"taxapplied_id": ta.ID,
		"tax_rate_id":   ta.TaxRateID,
		"entity_type":   ta.EntityType,
		"entity_id":     ta.EntityID,
		"tenant_id":     ta.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("creating taxapplied",
		"taxapplied_id", ta.ID,
		"tax_rate_id", ta.TaxRateID,
		"entity_type", ta.EntityType,
		"entity_id", ta.EntityID,
		"tenant_id", ta.TenantID,
	)

	// Set environment ID from context if not already set
	if ta.EnvironmentID == "" {
		ta.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.TaxApplied.Create().
		SetID(ta.ID).
		SetTenantID(ta.TenantID).
		SetTaxRateID(ta.TaxRateID).
		SetEntityType(string(ta.EntityType)).
		SetEntityID(ta.EntityID).
		SetNillableTaxAssociationID(ta.TaxAssociationID).
		SetTaxableAmount(ta.TaxableAmount).
		SetTaxAmount(ta.TaxAmount).
		SetCurrency(ta.Currency).
		SetJurisdiction(ta.Jurisdiction).
		SetAppliedAt(ta.AppliedAt).
		SetEnvironmentID(ta.EnvironmentID).
		SetMetadata(ta.Metadata).
		SetStatus(string(ta.Status)).
		SetCreatedAt(ta.CreatedAt).
		SetUpdatedAt(ta.UpdatedAt).
		SetCreatedBy(ta.CreatedBy).
		SetUpdatedBy(ta.UpdatedBy).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("error creating taxapplied", "error", err)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				// Handle specific constraint errors if any
				return ierr.WithError(err).
					WithHint("Tax applied record with same identifier already exists").
					WithReportableDetails(map[string]any{
						"taxapplied_id": ta.ID,
						"tax_rate_id":   ta.TaxRateID,
						"entity_id":     ta.EntityID,
						"error":         err.Error(),
					}).
					Mark(ierr.ErrAlreadyExists)
			}
		}
		return ierr.WithError(err).
			WithHint("Failed to create tax applied record").
			WithReportableDetails(map[string]any{
				"taxapplied_id": ta.ID,
				"tax_rate_id":   ta.TaxRateID,
				"entity_id":     ta.EntityID,
			}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *taxappliedRepository) Get(ctx context.Context, id string) (*domainTaxApplied.TaxApplied, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxapplied", "get", map[string]interface{}{
		"taxapplied_id": id,
	})
	defer FinishSpan(span)

	// Get from cache if exists
	if taxapplied := r.GetCache(ctx, id); taxapplied != nil {
		return taxapplied, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting taxapplied",
		"id", id,
	)

	taxappliedEnt, err := client.TaxApplied.Query().
		Where(
			taxapplied.ID(id),
			taxapplied.TenantID(types.GetTenantID(ctx)),
			taxapplied.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Tax applied record with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"taxapplied_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get tax applied record").
			Mark(ierr.ErrDatabase)
	}

	taxapplied := domainTaxApplied.FromEnt(taxappliedEnt)
	r.SetCache(ctx, taxapplied)

	return taxapplied, nil
}

func (r *taxappliedRepository) Update(ctx context.Context, ta *domainTaxApplied.TaxApplied) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxapplied", "update", map[string]interface{}{
		"taxapplied_id": ta.ID,
		"tax_rate_id":   ta.TaxRateID,
		"entity_id":     ta.EntityID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("updating taxapplied",
		"taxapplied_id", ta.ID,
		"tax_rate_id", ta.TaxRateID,
		"entity_id", ta.EntityID,
	)

	// Set environment ID from context if not already set
	if ta.EnvironmentID == "" {
		ta.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.TaxApplied.Update().
		Where(
			taxapplied.ID(ta.ID),
			taxapplied.TenantID(types.GetTenantID(ctx)),
			taxapplied.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetTaxableAmount(ta.TaxableAmount).
		SetTaxAmount(ta.TaxAmount).
		SetJurisdiction(ta.Jurisdiction).
		SetMetadata(ta.Metadata).
		SetStatus(string(ta.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("error updating taxapplied", "error", err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Tax applied record with ID %s was not found", ta.ID).
				WithReportableDetails(map[string]any{
					"taxapplied_id": ta.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update tax applied record").
			WithReportableDetails(map[string]any{
				"taxapplied_id": ta.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Update cache
	r.SetCache(ctx, ta)
	SetSpanSuccess(span)
	return nil
}

func (r *taxappliedRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxapplied", "delete", map[string]interface{}{
		"taxapplied_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("deleting taxapplied",
		"id", id,
	)

	// Get the record first to delete from cache
	t, err := r.Get(ctx, id)
	if err != nil {
		SetSpanError(span, err)
		return err
	}

	_, err = client.TaxApplied.Update().
		Where(
			taxapplied.ID(id),
			taxapplied.TenantID(types.GetTenantID(ctx)),
			taxapplied.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("error deleting taxapplied", "error", err)
		return ierr.WithError(err).
			WithHint("Failed to delete tax applied record").
			WithReportableDetails(map[string]any{
				"taxapplied_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Delete from cache
	r.DeleteCache(ctx, t)
	SetSpanSuccess(span)
	return nil
}

func (r *taxappliedRepository) List(ctx context.Context, filter *types.TaxAppliedFilter) ([]*domainTaxApplied.TaxApplied, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxapplied", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Invalid filter").
			Mark(ierr.ErrValidation)
	}

	query := r.client.Querier(ctx).TaxApplied.Query()

	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax applied records").
			Mark(ierr.ErrDatabase)
	}

	taxapplieds, err := query.All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list tax applied records").
			Mark(ierr.ErrDatabase)
	}

	r.log.Debugw("listing taxapplieds", "filter", filter)
	SetSpanSuccess(span)

	return domainTaxApplied.FromEntList(taxapplieds), nil
}

type TaxAppliedQuery = *ent.TaxAppliedQuery

type TaxAppliedQueryOptions struct{}

func (o TaxAppliedQueryOptions) ApplyTenantFilter(ctx context.Context, query TaxAppliedQuery) TaxAppliedQuery {
	return query.Where(taxapplied.TenantID(types.GetTenantID(ctx)))
}

func (o TaxAppliedQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query TaxAppliedQuery) TaxAppliedQuery {
	return query.Where(taxapplied.EnvironmentID(types.GetEnvironmentID(ctx)))
}

func (o TaxAppliedQueryOptions) ApplyStatusFilter(query TaxAppliedQuery, status string) TaxAppliedQuery {
	return query.Where(taxapplied.Status(status))
}

func (o TaxAppliedQueryOptions) ApplySortFilter(query TaxAppliedQuery, field string, order string) TaxAppliedQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o TaxAppliedQueryOptions) ApplyPaginationFilter(query TaxAppliedQuery, limit int, offset int) TaxAppliedQuery {
	return query.Limit(limit).Offset(offset)
}

func (o TaxAppliedQueryOptions) GetFieldName(field string) string {
	switch field {
	case "id":
		return taxapplied.FieldID
	case "tenant_id":
		return taxapplied.FieldTenantID
	case "status":
		return taxapplied.FieldStatus
	case "created_at":
		return taxapplied.FieldCreatedAt
	case "updated_at":
		return taxapplied.FieldUpdatedAt
	case "created_by":
		return taxapplied.FieldCreatedBy
	case "updated_by":
		return taxapplied.FieldUpdatedBy
	case "environment_id":
		return taxapplied.FieldEnvironmentID
	case "tax_rate_id":
		return taxapplied.FieldTaxRateID
	case "entity_type":
		return taxapplied.FieldEntityType
	case "entity_id":
		return taxapplied.FieldEntityID
	case "tax_association_id":
		return taxapplied.FieldTaxAssociationID
	case "taxable_amount":
		return taxapplied.FieldTaxableAmount
	case "tax_amount":
		return taxapplied.FieldTaxAmount
	case "currency":
		return taxapplied.FieldCurrency
	case "jurisdiction":
		return taxapplied.FieldJurisdiction
	case "applied_at":
		return taxapplied.FieldAppliedAt
	case "metadata":
		return taxapplied.FieldMetadata
	default:
		return field
	}
}

func (o TaxAppliedQueryOptions) GetFieldResolver(field string) (string, error) {
	field = o.GetFieldName(field)
	if field == "" {
		return "", ierr.WithError(errors.New("invalid field")).
			WithHint("Invalid field").
			Mark(ierr.ErrValidation)
	}
	return field, nil
}

func (o TaxAppliedQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.TaxAppliedFilter, query TaxAppliedQuery) (TaxAppliedQuery, error) {
	var predicates []predicate.TaxApplied

	// Apply tax rate IDs filter
	if len(f.TaxRateIDs) > 0 {
		predicates = append(predicates, taxapplied.TaxRateIDIn(f.TaxRateIDs...))
	}

	// Apply entity type filter
	if f.EntityType != "" {
		predicates = append(predicates, taxapplied.EntityType(string(f.EntityType)))
	}

	// Apply entity ID filter
	if f.EntityID != "" {
		predicates = append(predicates, taxapplied.EntityID(f.EntityID))
	}

	// Apply tax association ID filter
	if f.TaxAssociationID != "" {
		predicates = append(predicates, taxapplied.TaxAssociationID(f.TaxAssociationID))
	}

	// Apply custom filters
	if f.Filters != nil {
		var err error
		query, err = dsl.ApplyFilters[TaxAppliedQuery, predicate.TaxApplied](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.TaxApplied { return predicate.TaxApplied(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	if len(predicates) > 0 {
		query = query.Where(taxapplied.And(predicates...))
	}

	return query, nil
}

func (r *taxappliedRepository) SetCache(ctx context.Context, taxapplied *domainTaxApplied.TaxApplied) {
	span := cache.StartCacheSpan(ctx, "taxapplied", "set", map[string]interface{}{
		"taxapplied_id": taxapplied.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Set both ID and external ID based cache entries
	cacheKey := cache.GenerateKey(cache.PrefixTaxApplied, tenantID, environmentID, taxapplied.ID)
	r.cache.Set(ctx, cacheKey, taxapplied, cache.ExpiryDefaultInMemory)

	r.log.Debugw("cache set", "id_key", cacheKey)
}

func (r *taxappliedRepository) GetCache(ctx context.Context, key string) *domainTaxApplied.TaxApplied {
	span := cache.StartCacheSpan(ctx, "taxapplied", "get", map[string]interface{}{
		"taxapplied_id": key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixTaxApplied, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if taxapplied, ok := value.(*domainTaxApplied.TaxApplied); ok {
			r.log.Debugw("cache hit", "key", cacheKey)
			return taxapplied
		}
	}
	r.log.Debugw("cache miss", "key", cacheKey)
	return nil
}

func (r *taxappliedRepository) DeleteCache(ctx context.Context, taxapplied *domainTaxApplied.TaxApplied) {
	span := cache.StartCacheSpan(ctx, "taxapplied", "delete", map[string]interface{}{
		"taxapplied_id": taxapplied.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Delete ID-based cache first
	cacheKey := cache.GenerateKey(cache.PrefixTaxApplied, tenantID, environmentID, taxapplied.ID)
	r.cache.Delete(ctx, cacheKey)
	r.log.Debugw("cache deleted", "key", cacheKey)
}
