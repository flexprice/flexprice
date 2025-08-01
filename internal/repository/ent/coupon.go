package ent

import (
	"context"
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
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CouponQueryOptions
	cache     cache.Cache
}

func NewCouponRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCoupon.Repository {
	return &couponRepository{
		client:    client,
		log:       log,
		queryOpts: CouponQueryOptions{},
		cache:     cache,
	}
}

func (r *couponRepository) Create(ctx context.Context, c *domainCoupon.Coupon) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating coupon",
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
				WithHint("A coupon with this name already exists").
				WithReportableDetails(map[string]any{
					"name": c.Name,
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

	client := r.client.Querier(ctx)
	r.log.Debugw("getting coupon", "coupon_id", id)

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

func (r *couponRepository) GetBatch(ctx context.Context, ids []string) ([]*domainCoupon.Coupon, error) {
	if len(ids) == 0 {
		return []*domainCoupon.Coupon{}, nil
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "coupon", "get_batch", map[string]interface{}{
		"coupon_ids": ids,
		"count":      len(ids),
	})
	defer FinishSpan(span)

	r.log.Debugw("batch getting coupons", "coupon_ids", ids, "count", len(ids))

	// Check cache for each coupon first
	cachedCoupons := make(map[string]*domainCoupon.Coupon)
	uncachedIDs := make([]string, 0, len(ids))

	for _, id := range ids {
		if cachedCoupon := r.GetCache(ctx, id); cachedCoupon != nil {
			cachedCoupons[id] = cachedCoupon
		} else {
			uncachedIDs = append(uncachedIDs, id)
		}
	}

	r.log.Debugw("cache lookup results",
		"total_requested", len(ids),
		"cached_count", len(cachedCoupons),
		"uncached_count", len(uncachedIDs))

	// If all coupons are cached, return them
	if len(uncachedIDs) == 0 {
		result := make([]*domainCoupon.Coupon, 0, len(ids))
		for _, id := range ids {
			if coupon, exists := cachedCoupons[id]; exists {
				result = append(result, coupon)
			}
		}
		SetSpanSuccess(span)
		return result, nil
	}

	// Fetch uncached coupons from database
	client := r.client.Querier(ctx)

	entCoupons, err := client.Coupon.Query().
		Where(
			coupon.IDIn(uncachedIDs...),
			coupon.TenantID(types.GetTenantID(ctx)),
			coupon.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to batch get coupons").
			WithReportableDetails(map[string]interface{}{
				"coupon_ids": uncachedIDs,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain models and cache them
	fetchedCoupons := make(map[string]*domainCoupon.Coupon)
	for _, entCoupon := range entCoupons {
		domainCoupon := domainCoupon.FromEnt(entCoupon)
		fetchedCoupons[entCoupon.ID] = domainCoupon
		r.SetCache(ctx, domainCoupon)
	}

	// Combine cached and fetched coupons in the original order
	result := make([]*domainCoupon.Coupon, 0, len(ids))
	for _, id := range ids {
		if coupon, exists := cachedCoupons[id]; exists {
			result = append(result, coupon)
		} else if coupon, exists := fetchedCoupons[id]; exists {
			result = append(result, coupon)
		}
		// Note: We don't add nil for missing coupons to maintain consistency
		// Missing coupons are logged but not included in the result
	}

	r.log.Debugw("completed batch coupon fetch",
		"requested_count", len(ids),
		"fetched_count", len(entCoupons),
		"result_count", len(result))

	SetSpanSuccess(span)
	return result, nil
}

func (r *couponRepository) Update(ctx context.Context, c *domainCoupon.Coupon) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating coupon",
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
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting coupon",
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
	client := r.client.Querier(ctx)

	r.log.Debugw("incrementing coupon redemptions",
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
	client := r.client.Querier(ctx)

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
	client := r.client.Querier(ctx)

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

func (o CouponQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return coupon.FieldCreatedAt
	case "updated_at":
		return coupon.FieldUpdatedAt
	case "name":
		return coupon.FieldName
	case "type":
		return coupon.FieldType
	case "cadence":
		return coupon.FieldCadence
	case "currency":
		return coupon.FieldCurrency
	case "status":
		return coupon.FieldStatus
	case "redeem_after":
		return coupon.FieldRedeemAfter
	case "redeem_before":
		return coupon.FieldRedeemBefore
	case "total_redemptions":
		return coupon.FieldTotalRedemptions
	default:
		//unknown field
		return ""
	}
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

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cacheKey := cache.GenerateKey(cache.PrefixCoupon, tenantID, environmentID, coupon.ID)
	r.cache.Set(ctx, cacheKey, coupon, cache.ExpiryDefaultInMemory)

	r.log.Debugw("cache set", "key", cacheKey)
}

func (r *couponRepository) GetCache(ctx context.Context, key string) *domainCoupon.Coupon {
	span := cache.StartCacheSpan(ctx, "coupon", "get", map[string]interface{}{
		"coupon_id": key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixCoupon, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if coupon, ok := value.(*domainCoupon.Coupon); ok {
			r.log.Debugw("cache hit", "key", cacheKey)
			return coupon
		}
	}
	return nil
}

func (r *couponRepository) DeleteCache(ctx context.Context, coupon *domainCoupon.Coupon) {
	span := cache.StartCacheSpan(ctx, "coupon", "delete", map[string]interface{}{
		"coupon_id": coupon.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cacheKey := cache.GenerateKey(cache.PrefixCoupon, tenantID, environmentID, coupon.ID)
	r.cache.Delete(ctx, cacheKey)
	r.log.Debugw("cache deleted", "key", cacheKey)
}
