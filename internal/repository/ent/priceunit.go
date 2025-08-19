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

func (r *priceUnitRepository) Create(ctx context.Context, unit *domainPriceUnit.PriceUnit) error {

	r.log.Debugw("creating price unit",
		"price_unit_id", unit.ID,
		"tenant_id", unit.TenantID,
		"code", unit.Code,
	)

	span := StartRepositorySpan(ctx, "price_unit", "create", map[string]interface{}{
		"price_unit_id": unit.ID,
		"code":          unit.Code,
	})
	defer FinishSpan(span)
	client := r.client.Querier(ctx)

	// Create the price unit using the standard Ent API
	_, err := client.PriceUnit.Create().
		SetID(unit.ID).
		SetName(unit.Name).
		SetCode(unit.Code).
		SetSymbol(unit.Symbol).
		SetBaseCurrency(unit.BaseCurrency).
		SetConversionRate(unit.ConversionRate).
		SetPrecision(unit.Precision).
		SetCreatedBy(unit.CreatedBy).
		SetUpdatedBy(unit.UpdatedBy).
		SetStatus(string(types.StatusPublished)).
		SetTenantID(unit.TenantID).
		SetEnvironmentID(types.GetEnvironmentID(ctx)).
		SetCreatedAt(unit.CreatedAt).
		SetUpdatedAt(unit.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A pricing unit with this code already exists").
				WithReportableDetails(map[string]any{
					"code": unit.Code,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create pricing unit").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *priceUnitRepository) Get(ctx context.Context, id string) (*domainPriceUnit.PriceUnit, error) {
	span := StartRepositorySpan(ctx, "price_unit", "get", map[string]interface{}{
		"price_unit_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedUnit := r.GetCache(ctx, id); cachedUnit != nil {
		return cachedUnit, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting price unit",
		"price_unit_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	unit, err := client.PriceUnit.Query().
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
			WithHint("Failed to get pricing unit").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	priceUnitData := domainPriceUnit.FromEnt(unit)
	r.SetCache(ctx, priceUnitData)
	return priceUnitData, nil
}

func (r *priceUnitRepository) List(ctx context.Context, filter *domainPriceUnit.PriceUnitFilter) ([]*domainPriceUnit.PriceUnit, error) {
	if filter == nil {
		filter = &domainPriceUnit.PriceUnitFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	span := StartRepositorySpan(ctx, "price_unit", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	query := client.PriceUnit.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	units, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list pricing units").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainPriceUnit.FromEntList(units), nil
}

func (r *priceUnitRepository) Count(ctx context.Context, filter *domainPriceUnit.PriceUnitFilter) (int, error) {
	client := r.client.Querier(ctx)

	span := StartRepositorySpan(ctx, "price_unit", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.PriceUnit.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count pricing units").
			WithReportableDetails(map[string]any{
				"filter": filter,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *priceUnitRepository) Update(ctx context.Context, unit *domainPriceUnit.PriceUnit) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating price unit",
		"price_unit_id", unit.ID,
		"tenant_id", unit.TenantID,
	)

	span := StartRepositorySpan(ctx, "price_unit", "update", map[string]interface{}{
		"price_unit_id": unit.ID,
	})
	defer FinishSpan(span)

	_, err := client.PriceUnit.Update().
		Where(
			priceunit.ID(unit.ID),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(unit.Name).
		SetSymbol(unit.Symbol).
		SetStatus(string(unit.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price unit with ID %s was not found", unit.ID).
				WithReportableDetails(map[string]any{
					"price_unit_id": unit.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update pricing unit").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, unit.ID)
	return nil
}

func (r *priceUnitRepository) Delete(ctx context.Context, id string) error {

	r.log.Debugw("deleting price unit",
		"price_unit_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)
	span := StartRepositorySpan(ctx, "price_unit", "delete", map[string]interface{}{
		"price_unit_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	_, err := client.PriceUnit.Update().
		Where(
			priceunit.ID(id),
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
				WithHintf("Price unit with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"price_unit_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete pricing unit").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, id)
	return nil
}

func (r *priceUnitRepository) GetByCode(ctx context.Context, code string) (*domainPriceUnit.PriceUnit, error) {
	span := StartRepositorySpan(ctx, "price_unit", "get_by_code", map[string]interface{}{
		"code": code,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	unit, err := client.PriceUnit.Query().
		Where(
			priceunit.Code(code),
			priceunit.TenantID(types.GetTenantID(ctx)),
			priceunit.EnvironmentID(types.GetEnvironmentID(ctx)),
			priceunit.Status(string(types.StatusPublished)),
		).Only(ctx)
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
			WithHint("Failed to get pricing unit").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainPriceUnit.FromEnt(unit), nil
}

// PriceUnitQuery type alias for better readability
type PriceUnitQuery = *ent.PriceUnitQuery

// PriceUnitQueryOptions implements BaseQueryOptions for price unit queries
type PriceUnitQueryOptions struct{}

func (o PriceUnitQueryOptions) ApplyTenantFilter(ctx context.Context, query PriceUnitQuery) PriceUnitQuery {
	return query.Where(priceunit.TenantID(types.GetTenantID(ctx)))
}

func (o PriceUnitQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query PriceUnitQuery) PriceUnitQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(priceunit.EnvironmentID(environmentID))
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
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o PriceUnitQueryOptions) ApplyPaginationFilter(query PriceUnitQuery, limit int, offset int) PriceUnitQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
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
	case "status":
		return priceunit.FieldStatus
	default:
		return field
	}
}

func (o PriceUnitQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in price unit query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o PriceUnitQueryOptions) applyEntityQueryOptions(ctx context.Context, f *domainPriceUnit.PriceUnitFilter, query PriceUnitQuery) (PriceUnitQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(priceunit.CreatedAtGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(priceunit.CreatedAtLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	// Apply code filters if specified
	if len(f.Codes) > 0 {
		query = query.Where(priceunit.CodeIn(f.Codes...))
	}

	// Apply price unit IDs filter if specified
	if len(f.PriceUnitIDs) > 0 {
		query = query.Where(priceunit.IDIn(f.PriceUnitIDs...))
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
	r.cache.Set(ctx, cacheKey, priceUnit, cache.ExpiryDefaultInMemory)
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

func (r *priceUnitRepository) DeleteCache(ctx context.Context, priceUnitID string) {
	span := cache.StartCacheSpan(ctx, "price_unit", "delete", map[string]interface{}{
		"price_unit_id": priceUnitID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPriceUnit, tenantID, environmentID, priceUnitID)
	r.cache.Delete(ctx, cacheKey)
}
