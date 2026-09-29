package ent

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/fxrate"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/cache"
	domainFXRate "github.com/flexprice/flexprice/internal/domain/fxrate"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type fxRateRepository struct {
	client     postgres.IClient
	log        *logger.Logger
	queryOpts  FXRateQueryOptions
	redisCache cache.RedisCache
}

func NewFXRateRepository(client postgres.IClient, log *logger.Logger, redisCache cache.RedisCache) domainFXRate.Repository {
	return &fxRateRepository{
		client:     client,
		log:        log,
		queryOpts:  FXRateQueryOptions{},
		redisCache: redisCache,
	}
}

func (r *fxRateRepository) Create(ctx context.Context, fr *domainFXRate.FXRate) error {
	span := StartRepositorySpan(ctx, "fxrate", "create", map[string]interface{}{
		"fx_rate_id": fr.ID,
		"scope":      fr.Scope,
		"tenant_id":  fr.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	if fr.EnvironmentID == "" {
		fr.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.FXRate.Create().
		SetID(fr.ID).
		SetScope(string(fr.Scope)).
		SetScopeID(fr.ScopeID).
		SetFromCurrency(strings.ToLower(fr.FromCurrency)).
		SetToCurrency(strings.ToLower(fr.ToCurrency)).
		SetRate(fr.Rate).
		SetNillableValidFrom(fr.ValidFrom).
		SetNillableValidTo(fr.ValidTo).
		SetMetadata(fr.Metadata).
		SetStatus(string(fr.Status)).
		SetTenantID(fr.TenantID).
		SetCreatedBy(fr.CreatedBy).
		SetEnvironmentID(fr.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.log.Error(ctx, "error creating fx rate", "error", err, "fx_rate_id", fr.ID)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Idx_fx_rate_tenant_live {
					return ierr.WithError(err).
						WithHintf("A tenant FX rate for %s → %s already exists", fr.FromCurrency, fr.ToCurrency).
						WithReportableDetails(map[string]any{
							"from_currency": fr.FromCurrency,
							"to_currency":   fr.ToCurrency,
							"fx_rate_id":    fr.ID,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
		}
		return ierr.WithError(err).
			WithHint("Failed to create fx rate").
			WithReportableDetails(map[string]any{"fx_rate_id": fr.ID}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *fxRateRepository) Get(ctx context.Context, id string) (*domainFXRate.FXRate, error) {
	span := StartRepositorySpan(ctx, "fxrate", "get", map[string]interface{}{"fx_rate_id": id})
	defer FinishSpan(span)

	if cached := r.GetCache(ctx, id); cached != nil {
		return cached, nil
	}

	client := r.client.Reader(ctx)
	entRate, err := client.FXRate.Query().
		Where(
			fxrate.ID(id),
			fxrate.TenantID(types.GetTenantID(ctx)),
			fxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("FX rate with ID %s was not found", id).
				WithReportableDetails(map[string]any{"fx_rate_id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("Failed to get fx rate").Mark(ierr.ErrDatabase)
	}

	result := domainFXRate.FromEnt(entRate)
	r.SetCache(ctx, result)
	return result, nil
}

func (r *fxRateRepository) List(ctx context.Context, filter *types.FXRateFilter) ([]*domainFXRate.FXRate, error) {
	span := StartRepositorySpan(ctx, "fxrate", "list", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Invalid filter").Mark(ierr.ErrValidation)
	}

	query := r.client.Reader(ctx).FXRate.Query()
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to list fx rates").Mark(ierr.ErrDatabase)
	}

	rates, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to list fx rates").Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainFXRate.FromEntList(rates), nil
}

func (r *fxRateRepository) ListAll(ctx context.Context, filter *types.FXRateFilter) ([]*domainFXRate.FXRate, error) {
	span := StartRepositorySpan(ctx, "fxrate", "list_all", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	client := r.client.Reader(ctx)
	rates, err := client.FXRate.Query().
		Where(
			fxrate.TenantID(types.GetTenantID(ctx)),
			fxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to list all fx rates").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainFXRate.FromEntList(rates), nil
}

func (r *fxRateRepository) Count(ctx context.Context, filter *types.FXRateFilter) (int, error) {
	span := StartRepositorySpan(ctx, "fxrate", "count", map[string]interface{}{"filter": filter})
	defer FinishSpan(span)

	query := r.client.Reader(ctx).FXRate.Query()
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).WithHint("Failed to count fx rates").Mark(ierr.ErrDatabase)
	}
	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).WithHint("Failed to count fx rates").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return count, nil
}

func (r *fxRateRepository) Update(ctx context.Context, fr *domainFXRate.FXRate) error {
	span := StartRepositorySpan(ctx, "fxrate", "update", map[string]interface{}{"fx_rate_id": fr.ID})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	update := client.FXRate.Update().
		Where(
			fxrate.ID(fr.ID),
			fxrate.TenantID(types.GetTenantID(ctx)),
			fxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetRate(fr.Rate).
		SetMetadata(fr.Metadata).
		SetStatus(string(fr.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx))

	if fr.ValidFrom != nil {
		update.SetValidFrom(*fr.ValidFrom)
	} else {
		update.ClearValidFrom()
	}
	if fr.ValidTo != nil {
		update.SetValidTo(*fr.ValidTo)
	} else {
		update.ClearValidTo()
	}

	_, err := update.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		r.log.Error(ctx, "error updating fx rate", "error", err, "fx_rate_id", fr.ID)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("FX rate with ID %s was not found", fr.ID).
				WithReportableDetails(map[string]any{"fx_rate_id": fr.ID}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).WithHint("Failed to update fx rate").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	r.DeleteCache(ctx, fr)
	return nil
}

// Delete soft-archives the rate by setting its status to archived.
func (r *fxRateRepository) Delete(ctx context.Context, fr *domainFXRate.FXRate) error {
	span := StartRepositorySpan(ctx, "fxrate", "delete", map[string]interface{}{"fx_rate_id": fr.ID})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	_, err := client.FXRate.Update().
		Where(
			fxrate.ID(fr.ID),
			fxrate.TenantID(types.GetTenantID(ctx)),
			fxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).WithHint("Failed to delete fx rate").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	r.DeleteCache(ctx, fr)
	return nil
}

func (r *fxRateRepository) GetTenantRate(ctx context.Context, from, to string) (*domainFXRate.FXRate, error) {
	span := StartRepositorySpan(ctx, "fxrate", "get_tenant_rate", map[string]interface{}{"from": from, "to": to})
	defer FinishSpan(span)

	entRate, err := r.client.Reader(ctx).FXRate.Query().
		Where(
			fxrate.Scope(string(types.FXRateScopeTenant)),
			fxrate.ScopeID(types.FXRateScopeIDTenant),
			fxrate.FromCurrency(strings.ToLower(from)),
			fxrate.ToCurrency(strings.ToLower(to)),
			fxrate.Status(string(types.StatusPublished)),
			fxrate.TenantID(types.GetTenantID(ctx)),
			fxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("No tenant FX rate configured for %s → %s", from, to).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).WithHint("Failed to get tenant fx rate").Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return domainFXRate.FromEnt(entRate), nil
}

func (r *fxRateRepository) FindOverlapping(ctx context.Context, scope types.FXRateScope, scopeID, from, to string, validFrom, validTo *time.Time, excludeID string) ([]*domainFXRate.FXRate, error) {
	span := StartRepositorySpan(ctx, "fxrate", "find_overlapping", map[string]interface{}{"scope": scope, "scope_id": scopeID})
	defer FinishSpan(span)

	preds := []predicate.FXRate{
		fxrate.Scope(string(scope)),
		fxrate.ScopeID(scopeID),
		fxrate.FromCurrency(strings.ToLower(from)),
		fxrate.ToCurrency(strings.ToLower(to)),
		fxrate.Status(string(types.StatusPublished)),
		fxrate.TenantID(types.GetTenantID(ctx)),
		fxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
	}
	if excludeID != "" {
		preds = append(preds, fxrate.IDNEQ(excludeID))
	}

	rates, err := r.client.Reader(ctx).FXRate.Query().Where(preds...).All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).WithHint("Failed to check overlapping fx rates").Mark(ierr.ErrDatabase)
	}

	overlapping := make([]*domainFXRate.FXRate, 0)
	for _, e := range rates {
		if windowsOverlap(e.ValidFrom, e.ValidTo, validFrom, validTo) {
			overlapping = append(overlapping, domainFXRate.FromEnt(e))
		}
	}
	SetSpanSuccess(span)
	return overlapping, nil
}

// windowsOverlap reports whether two half-open [from, to) windows intersect.
// A nil from is −∞ and a nil to is +∞.
func windowsOverlap(aFrom, aTo, bFrom, bTo *time.Time) bool {
	// a starts before b ends, and b starts before a ends
	if aFrom != nil && bTo != nil && !aFrom.Before(*bTo) {
		return false
	}
	if bFrom != nil && aTo != nil && !bFrom.Before(*aTo) {
		return false
	}
	return true
}

// FXRateQuery type alias for readability.
type FXRateQuery = *ent.FXRateQuery

// FXRateQueryOptions implements BaseQueryOptions for fx rate queries.
type FXRateQueryOptions struct{}

func (o FXRateQueryOptions) ApplyTenantFilter(ctx context.Context, query FXRateQuery) FXRateQuery {
	return query.Where(fxrate.TenantID(types.GetTenantID(ctx)))
}

func (o FXRateQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query FXRateQuery) FXRateQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(fxrate.EnvironmentID(environmentID))
	}
	return query
}

func (o FXRateQueryOptions) ApplyStatusFilter(query FXRateQuery, status string) FXRateQuery {
	if status == "" {
		return query.Where(fxrate.StatusEQ(string(types.StatusPublished)))
	}
	return query.Where(fxrate.Status(status))
}

func (o FXRateQueryOptions) ApplySortFilter(query FXRateQuery, field string, order string) FXRateQuery {
	field = o.GetFieldName(field)
	if order == types.OrderDesc {
		return query.Order(ent.Desc(field))
	}
	return query.Order(ent.Asc(field))
}

func (o FXRateQueryOptions) ApplyPaginationFilter(query FXRateQuery, limit int, offset int) FXRateQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o FXRateQueryOptions) GetFieldName(field string) string {
	if fxrate.ValidColumn(field) {
		return field
	}
	return ""
}

func (o FXRateQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in fxrate query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o FXRateQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.FXRateFilter, query FXRateQuery) (FXRateQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	if len(f.FXRateIDs) > 0 {
		query = query.Where(fxrate.IDIn(f.FXRateIDs...))
	}
	if f.Scope != nil {
		query = query.Where(fxrate.Scope(string(*f.Scope)))
	}
	if f.ScopeID != nil {
		query = query.Where(fxrate.ScopeID(*f.ScopeID))
	}
	if f.FromCurrency != nil {
		query = query.Where(fxrate.FromCurrency(strings.ToLower(*f.FromCurrency)))
	}
	if f.ToCurrency != nil {
		query = query.Where(fxrate.ToCurrency(strings.ToLower(*f.ToCurrency)))
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[FXRateQuery, predicate.FXRate](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.FXRate { return predicate.FXRate(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	if f.Sort != nil {
		query, err = dsl.ApplySorts[FXRateQuery, fxrate.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(fn dsl.OrderFunc) fxrate.OrderOption { return fxrate.OrderOption(fn) },
		)
		if err != nil {
			return nil, err
		}
	}
	return query, nil
}

// caching

func (r *fxRateRepository) SetCache(ctx context.Context, fr *domainFXRate.FXRate) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "fxrate", "set", map[string]interface{}{"fx_rate_id": fr.ID})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixFXRate, fr.ID)
	r.redisCache.Set(ctx, cacheKey, fr, cache.ExpiryDefaultRedis)
}

func (r *fxRateRepository) GetCache(ctx context.Context, id string) *domainFXRate.FXRate {
	span, ctx := cache.StartRedisCacheSpan(ctx, "fxrate", "get", map[string]interface{}{"fx_rate_id": id})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixFXRate, id)
	value, found := r.redisCache.Get(ctx, cacheKey)
	if !found {
		return nil
	}
	fr, ok := cache.UnmarshalCacheValue[domainFXRate.FXRate](value)
	if !ok {
		return nil
	}
	return fr
}

func (r *fxRateRepository) DeleteCache(ctx context.Context, fr *domainFXRate.FXRate) {
	span, ctx := cache.StartRedisCacheSpan(ctx, "fxrate", "delete", map[string]interface{}{"fx_rate_id": fr.ID})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixFXRate, fr.ID)
	r.redisCache.Delete(ctx, cacheKey)
}
