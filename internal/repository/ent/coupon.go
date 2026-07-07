package ent

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/coupon"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domainCoupon "github.com/flexprice/flexprice/internal/domain/coupon"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type couponRepository struct {
	client     postgres.IClient
	log        *logger.Logger
	queryOpts  CouponQueryOptions
	redisCache cache.RedisCache
}

func NewCouponRepository(client postgres.IClient, log *logger.Logger, redisCache cache.RedisCache) domainCoupon.Repository {
	return &couponRepository{
		client:     client,
		log:        log,
		queryOpts:  CouponQueryOptions{},
		redisCache: redisCache,
	}
}

func (r *couponRepository) Create(ctx context.Context, c *domainCoupon.Coupon) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "creating coupon",
		"coupon_id", c.ID,
		"tenant_id", c.TenantID,
		"name", c.Name,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon", "create", map[string]interface{}{
		"coupon_id": c.ID,
		"name":      c.Name,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	createQuery := client.Coupon.Create().
		SetID(c.ID).
		SetTenantID(c.TenantID).
		SetName(c.Name).
		SetType(string(c.Type)).
		SetCadence(string(c.Cadence)).
		SetStatus(string(c.Status)).
		SetCreatedAt(c.CreatedAt).
		SetUpdatedAt(c.UpdatedAt).
		SetCreatedBy(c.CreatedBy).
		SetUpdatedBy(c.UpdatedBy).
		SetEnvironmentID(c.EnvironmentID).
		SetCurrency(c.Currency).
		SetNillableAmountOff(c.AmountOff).
		SetNillablePercentageOff(c.PercentageOff).
		SetNillableRedeemAfter(c.RedeemAfter).
		SetNillableRedeemBefore(c.RedeemBefore).
		SetNillableMaxRedemptions(c.MaxRedemptions).
		SetNillableTotalRedemptions(lo.ToPtr(c.TotalRedemptions)).
		SetNillableDurationInPeriods(c.DurationInPeriods)

	// Handle optional fields
	if code := strings.ToLower(strings.TrimSpace(lo.FromPtr(c.CouponCode))); code != "" {
		createQuery = createQuery.SetCouponCode(code)
	}
	if c.Rules != nil {
		createQuery = createQuery.SetRules(*c.Rules)
	}
	if c.Metadata != nil {
		createQuery = createQuery.SetMetadata(*c.Metadata)
	}

	coupon, err := createQuery.Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A published coupon with this code already exists in this environment").
				WithReportableDetails(map[string]any{
					"name":        c.Name,
					"coupon_code": c.CouponCode,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create coupon").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*c = *domainCoupon.FromEnt(coupon)
	return nil
}

func (r *couponRepository) Get(ctx context.Context, id string) (*domainCoupon.Coupon, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon", "get", map[string]interface{}{
		"coupon_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedCoupon := r.GetCache(ctx, id); cachedCoupon != nil {
		return cachedCoupon, nil
	}

	client := r.client.Reader(ctx)
	r.log.Debug(ctx, "getting coupon", "coupon_id", id)

	c, err := client.Coupon.Query().
		Where(
			coupon.ID(id),
			coupon.TenantID(types.GetTenantID(ctx)),
			coupon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Coupon with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"coupon_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	coupon := domainCoupon.FromEnt(c)
	r.SetCache(ctx, coupon)
	return coupon, nil
}

func (r *couponRepository) GetByCode(ctx context.Context, code string) (*domainCoupon.Coupon, error) {
	span := StartRepositorySpan(ctx, "coupon", "get_by_code", map[string]interface{}{
		"coupon_code": code,
	})
	defer FinishSpan(span)

	normalised := strings.ToLower(strings.TrimSpace(code))
	if normalised == "" {
		return nil, ierr.NewError("coupon_code is required").
			Mark(ierr.ErrValidation)
	}

	client := r.client.Reader(ctx)
	c, err := client.Coupon.Query().
		Where(
			coupon.CouponCode(normalised),
			coupon.TenantID(types.GetTenantID(ctx)),
			coupon.EnvironmentID(types.GetEnvironmentID(ctx)),
			coupon.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Coupon with code '%s' was not found", code).
				WithReportableDetails(map[string]any{"coupon_code": code}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get coupon by code").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCoupon.FromEnt(c), nil
}

func (r *couponRepository) Update(ctx context.Context, c *domainCoupon.Coupon) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "updating coupon",
		"coupon_id", c.ID,
		"tenant_id", c.TenantID,
		"name", c.Name,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon", "update", map[string]interface{}{
		"coupon_id": c.ID,
		"name":      c.Name,
	})
	defer FinishSpan(span)

	updateQuery := client.Coupon.Update().
		Where(
			coupon.ID(c.ID),
			coupon.TenantID(c.TenantID),
			coupon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(c.Name).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx))

	if c.CouponCode != nil {
		if *c.CouponCode == "" {
			updateQuery = updateQuery.ClearCouponCode()
		} else {
			updateQuery = updateQuery.SetCouponCode(strings.ToLower(strings.TrimSpace(*c.CouponCode)))
		}
	}
	if c.Metadata != nil {
		updateQuery = updateQuery.SetMetadata(*c.Metadata)
	}
	_, err := updateQuery.Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Coupon with ID %s was not found", c.ID).
				WithReportableDetails(map[string]any{
					"coupon_id": c.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A coupon with this name already exists").
				WithReportableDetails(map[string]any{
					"name": c.Name,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to update coupon").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, c)
	return nil
}

func (r *couponRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "deleting coupon",
		"coupon_id", id,
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon", "delete", map[string]interface{}{
		"coupon_id": id,
	})
	defer FinishSpan(span)

	_, err := client.Coupon.Update().
		Where(
			coupon.ID(id),
			coupon.TenantID(types.GetTenantID(ctx)),
			coupon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Coupon with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"coupon_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete coupon").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, &domainCoupon.Coupon{ID: id})
	return nil
}

func (r *couponRepository) IncrementRedemptions(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	r.log.Debug(ctx, "incrementing coupon redemptions",
		"coupon_id", id,
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon", "increment_redemptions", map[string]interface{}{
		"coupon_id": id,
	})
	defer FinishSpan(span)

	_, err := client.Coupon.Update().
		Where(
			coupon.ID(id),
			coupon.TenantID(types.GetTenantID(ctx)),
			coupon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		AddTotalRedemptions(1).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Coupon with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"coupon_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to increment coupon redemptions").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, &domainCoupon.Coupon{ID: id})
	return nil
}

func (r *couponRepository) List(ctx context.Context, filter *types.CouponFilter) ([]*domainCoupon.Coupon, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Coupon.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupons").
			Mark(ierr.ErrDatabase)
	}
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	coupons, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list coupons").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCoupon.FromEntList(coupons), nil
}

func (r *couponRepository) Count(ctx context.Context, filter *types.CouponFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Coupon.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count coupons").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// CouponQuery type alias for better readability
type CouponQuery = *ent.CouponQuery

// CouponQueryOptions implements BaseQueryOptions for coupon queries
type CouponQueryOptions struct{}

func (o CouponQueryOptions) ApplyTenantFilter(ctx context.Context, query CouponQuery) CouponQuery {
	return query.Where(coupon.TenantIDEQ(types.GetTenantID(ctx)))
}

func (o CouponQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CouponQuery) CouponQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(coupon.EnvironmentIDEQ(environmentID))
	}
	return query
}

func (o CouponQueryOptions) ApplyStatusFilter(query CouponQuery, status string) CouponQuery {
	if status == "" {
		return query.Where(coupon.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(coupon.Status(status))
}

func (o CouponQueryOptions) ApplySortFilter(query CouponQuery, field string, order string) CouponQuery {
	if field != "" {
		if order == types.OrderDesc {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o CouponQueryOptions) ApplyPaginationFilter(query CouponQuery, limit int, offset int) CouponQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName returns the ent field name for coupon; delegates to ent's ValidColumn so new schema fields are supported automatically.
func (o CouponQueryOptions) GetFieldName(field string) string {
	if coupon.ValidColumn(field) {
		return field
	}
	return ""
}

func (o CouponQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in coupon query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o CouponQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CouponFilter, query CouponQuery) (CouponQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	if len(f.CouponIDs) > 0 {
		query = query.Where(coupon.IDIn(f.CouponIDs...))
	}

	if len(f.CouponCodes) > 0 {
		query = query.Where(coupon.CouponCodeIn(f.CouponCodes...))
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[CouponQuery, predicate.Coupon](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Coupon { return predicate.Coupon(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[CouponQuery, coupon.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) coupon.OrderOption { return coupon.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (r *couponRepository) SetCache(ctx context.Context, coupon *domainCoupon.Coupon) {
	span := cache.StartCacheSpan(ctx, "coupon", "set", map[string]interface{}{
		"coupon_id": coupon.ID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixCoupon, coupon.ID)
	r.redisCache.Set(ctx, cacheKey, coupon, cache.ExpiryDefaultRedis)
}

func (r *couponRepository) GetCache(ctx context.Context, id string) *domainCoupon.Coupon {
	span := cache.StartCacheSpan(ctx, "coupon", "get", map[string]interface{}{
		"coupon_id": id,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixCoupon, id)
	value, found := r.redisCache.Get(ctx, cacheKey)
	if !found {
		return nil
	}
	c, ok := cache.UnmarshalCacheValue[domainCoupon.Coupon](value)
	if !ok {
		return nil
	}
	return c
}

func (r *couponRepository) DeleteCache(ctx context.Context, coupon *domainCoupon.Coupon) {
	span := cache.StartCacheSpan(ctx, "coupon", "delete", map[string]interface{}{
		"coupon_id": coupon.ID,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(ctx, cache.PrefixCoupon, coupon.ID)
	r.redisCache.Delete(ctx, cacheKey)
}
