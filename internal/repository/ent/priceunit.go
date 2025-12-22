package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/priceunit"
	"github.com/flexprice/flexprice/internal/cache"
	domainPriceUnit "github.com/flexprice/flexprice/internal/domain/priceunit"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type priceUnitRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts PriceUnitQueryOptions
	cache     cache.Cache
}

func NewPriceUnitRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainPriceUnit.Repository {
	return &priceUnitRepository{
		client:    client,
		log:       log,
		queryOpts: PriceUnitQueryOptions{},
		cache:     cache,
	}
}

func (r *priceUnitRepository) Create(ctx context.Context, priceUnit *domainPriceUnit.PriceUnit) (*domainPriceUnit.PriceUnit, error) {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating price unit",
		"price_unit_id", priceUnit.ID,
		"tenant_id", priceUnit.TenantID,
		"name", priceUnit.Name,
		"code", priceUnit.Code,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price_unit", "create", map[string]interface{}{
		"price_unit_id": priceUnit.ID,
		"name":          priceUnit.Name,
		"code":          priceUnit.Code,
	})
	defer FinishSpan(span)

	entPriceUnit, err := client.PriceUnit.Create().
		SetID(priceUnit.ID).
		SetName(priceUnit.Name).
		SetCode(priceUnit.Code).
		SetSymbol(priceUnit.Symbol).
		SetBaseCurrency(priceUnit.BaseCurrency).
		SetConversionRate(priceUnit.ConversionRate).
		SetTenantID(priceUnit.TenantID).
		SetStatus(string(priceUnit.Status)).
		SetCreatedAt(priceUnit.CreatedAt).
		SetUpdatedAt(priceUnit.UpdatedAt).
		SetCreatedBy(priceUnit.CreatedBy).
		SetUpdatedBy(priceUnit.UpdatedBy).
		SetEnvironmentID(priceUnit.EnvironmentID).
		SetMetadata(priceUnit.Metadata).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return nil, ierr.WithError(err).
				WithHint("Price unit with this code already exists").
				WithReportableDetails(map[string]any{
					"price_unit_id": priceUnit.ID,
					"code":          priceUnit.Code,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to create price unit").
			WithReportableDetails(map[string]any{
				"price_unit_id": priceUnit.ID,
				"code":          priceUnit.Code,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	result := domainPriceUnit.FromEnt(entPriceUnit)
	r.SetCache(ctx, result)
	return result, nil
}

func (r *priceUnitRepository) Get(ctx context.Context, id string) (*domainPriceUnit.PriceUnit, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price_unit", "get", map[string]interface{}{
		"price_unit_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedPriceUnit := r.GetCache(ctx, id); cachedPriceUnit != nil {
		return cachedPriceUnit, nil
	}

	client := r.client.Reader(ctx)

	r.log.Debugw("getting price unit",
		"price_unit_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	entPriceUnit, err := client.PriceUnit.Query().
		Where(
			priceunit.ID(id),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Price unit with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"price_unit_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHintf("Failed to get price unit with ID %s", id).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	priceUnitData := domainPriceUnit.FromEnt(entPriceUnit)
	r.SetCache(ctx, priceUnitData)
	return priceUnitData, nil
}

func (r *priceUnitRepository) List(ctx context.Context, filter *types.PriceUnitFilter) ([]*domainPriceUnit.PriceUnit, error) {
	if filter == nil {
		filter = &types.PriceUnitFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price_unit", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	query := client.PriceUnit.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return nil, err
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	entPriceUnits, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list price units").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainPriceUnit.FromEntList(entPriceUnits), nil
}

func (r *priceUnitRepository) Count(ctx context.Context, filter *types.PriceUnitFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price_unit", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.PriceUnit.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count price units").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *priceUnitRepository) Update(ctx context.Context, priceUnit *domainPriceUnit.PriceUnit) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating price unit",
		"price_unit_id", priceUnit.ID,
		"tenant_id", priceUnit.TenantID,
		"code", priceUnit.Code,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price_unit", "update", map[string]interface{}{
		"price_unit_id": priceUnit.ID,
		"code":          priceUnit.Code,
	})
	defer FinishSpan(span)

	_, err := client.PriceUnit.Update().
		Where(
			priceunit.ID(priceUnit.ID),
			priceunit.TenantID(priceUnit.TenantID),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(priceUnit.Name).
		SetSymbol(priceUnit.Symbol).
		SetStatus(string(priceUnit.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetMetadata(priceUnit.Metadata).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price unit with ID %s was not found", priceUnit.ID).
				WithReportableDetails(map[string]any{
					"price_unit_id": priceUnit.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("Price unit with this code already exists").
				WithReportableDetails(map[string]any{
					"price_unit_id": priceUnit.ID,
					"code":          priceUnit.Code,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to update price unit").
			WithReportableDetails(map[string]any{
				"price_unit_id": priceUnit.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, priceUnit)
	return nil
}

func (r *priceUnitRepository) Delete(ctx context.Context, priceUnit *domainPriceUnit.PriceUnit) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("deleting price unit",
		"price_unit_id", priceUnit.ID,
		"tenant_id", priceUnit.TenantID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price_unit", "delete", map[string]interface{}{
		"price_unit_id": priceUnit.ID,
	})
	defer FinishSpan(span)

	_, err := client.PriceUnit.Update().
		Where(
			priceunit.ID(priceUnit.ID),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price unit with ID %s was not found", priceUnit.ID).
				WithReportableDetails(map[string]any{
					"price_unit_id": priceUnit.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete price unit").
			WithReportableDetails(map[string]any{
				"price_unit_id": priceUnit.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, priceUnit)
	return nil
}

func (r *priceUnitRepository) GetByCode(ctx context.Context, code string) (*domainPriceUnit.PriceUnit, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price_unit", "get_by_code", map[string]interface{}{
		"code": code,
	})
	defer FinishSpan(span)

	r.log.Debugw("getting price unit by code", "code", code)

	// Try to get from cache first
	if cachedPriceUnit := r.GetCache(ctx, code); cachedPriceUnit != nil {
		return cachedPriceUnit, nil
	}

	entPriceUnit, err := r.client.Reader(ctx).PriceUnit.Query().
		Where(
			priceunit.Code(code),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Price unit with code %s was not found", code).
				WithReportableDetails(map[string]any{
					"code": code,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get price unit by code").
			WithReportableDetails(map[string]any{
				"code": code,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	priceUnitData := domainPriceUnit.FromEnt(entPriceUnit)
	r.SetCache(ctx, priceUnitData)
	return priceUnitData, nil
}

// PriceUnitQuery type alias for better readability
type PriceUnitQuery = *ent.PriceUnitQuery

// PriceUnitQueryOptions implements query options for price unit filtering and sorting
type PriceUnitQueryOptions struct{}

func (o PriceUnitQueryOptions) ApplyTenantFilter(ctx context.Context, query PriceUnitQuery) PriceUnitQuery {
	return query.Where(priceunit.TenantID(types.GetTenantID(ctx)))
}

func (o PriceUnitQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query PriceUnitQuery) PriceUnitQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(priceunit.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o PriceUnitQueryOptions) ApplyStatusFilter(query PriceUnitQuery, status string) PriceUnitQuery {
	if status == "" {
		return query.Where(priceunit.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(priceunit.Status(status))
}

func (o PriceUnitQueryOptions) ApplySortFilter(query PriceUnitQuery, field string, order string) PriceUnitQuery {
	field = o.GetFieldName(field)

	// Apply standard ordering for all fields
	if order == types.OrderDesc {
		query = query.Order(ent.Desc(field))
	} else {
		query = query.Order(ent.Asc(field))
	}
	return query
}

func (o PriceUnitQueryOptions) ApplyPaginationFilter(query PriceUnitQuery, limit int, offset int) PriceUnitQuery {
	return query.Offset(offset).Limit(limit)
}

func (o PriceUnitQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return priceunit.FieldCreatedAt
	case "updated_at":
		return priceunit.FieldUpdatedAt
	case "name":
		return priceunit.FieldName
	case "code":
		return priceunit.FieldCode
	case "symbol":
		return priceunit.FieldSymbol
	case "base_currency":
		return priceunit.FieldBaseCurrency
	case "conversion_rate":
		return priceunit.FieldConversionRate
	case "status":
		return priceunit.FieldStatus
	default:
		// unknown field
		return ""
	}
}

func (o PriceUnitQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in price unit query", field).
			WithHintf("Unknown field name '%s' in price unit query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o PriceUnitQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.PriceUnitFilter, query PriceUnitQuery) (PriceUnitQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply price unit IDs filter if specified
	if len(f.PriceUnitIDs) > 0 {
		query = query.Where(priceunit.IDIn(f.PriceUnitIDs...))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(priceunit.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(priceunit.CreatedAtLTE(*f.EndTime))
		}
	}

	// Apply filters using the generic function
	if f.Filters != nil {
		query, err = dsl.ApplyFilters[PriceUnitQuery, predicate.PriceUnit](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.PriceUnit { return predicate.PriceUnit(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[PriceUnitQuery, priceunit.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) priceunit.OrderOption { return priceunit.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (r *priceUnitRepository) SetCache(ctx context.Context, priceUnit *domainPriceUnit.PriceUnit) {
	span := cache.StartCacheSpan(ctx, "price_unit", "set", map[string]interface{}{
		"price_unit_id": priceUnit.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPriceUnit, tenantID, environmentID, priceUnit.ID)
	codeCacheKey := cache.GenerateKey(cache.PrefixPriceUnit, tenantID, environmentID, priceUnit.Code)
	r.cache.Set(ctx, cacheKey, priceUnit, cache.ExpiryDefaultInMemory)
	r.cache.Set(ctx, codeCacheKey, priceUnit, cache.ExpiryDefaultInMemory)
}

func (r *priceUnitRepository) GetCache(ctx context.Context, key string) *domainPriceUnit.PriceUnit {
	span := cache.StartCacheSpan(ctx, "price_unit", "get", map[string]interface{}{
		"price_unit_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPriceUnit, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainPriceUnit.PriceUnit)
	}
	return nil
}

func (r *priceUnitRepository) DeleteCache(ctx context.Context, priceUnit *domainPriceUnit.PriceUnit) {
	span := cache.StartCacheSpan(ctx, "price_unit", "delete", map[string]interface{}{
		"price_unit_id": priceUnit.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPriceUnit, tenantID, environmentID, priceUnit.ID)
	codeCacheKey := cache.GenerateKey(cache.PrefixPriceUnit, tenantID, environmentID, priceUnit.Code)
	r.cache.Delete(ctx, cacheKey)
	r.cache.Delete(ctx, codeCacheKey)
}
