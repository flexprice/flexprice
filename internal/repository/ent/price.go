package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/price"
	"github.com/flexprice/flexprice/internal/cache"
	domainPrice "github.com/flexprice/flexprice/internal/domain/price"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type priceRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts PriceQueryOptions
	cache     cache.Cache
}

func NewPriceRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainPrice.Repository {
	return &priceRepository{
		client:    client,
		log:       log,
		queryOpts: PriceQueryOptions{},
		cache:     cache,
	}
}

func (r *priceRepository) Create(ctx context.Context, p *domainPrice.Price) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating price",
		"price_id", p.ID,
		"tenant_id", p.TenantID,
		"lookup_key", p.LookupKey,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price", "create", map[string]interface{}{
		"price_id":  p.ID,
		"tenant_id": p.TenantID,

		"lookup_key": p.LookupKey,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if p.EnvironmentID == "" {
		p.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Create the price using the standard Ent API
	priceBuilder := client.Price.Create().
		SetID(p.ID).
		SetTenantID(p.TenantID).
		SetAmount(p.Amount).
		SetCurrency(p.Currency).
		SetDisplayAmount(p.DisplayAmount).
		SetType(p.Type).
		SetPriceUnitType(string(p.PriceUnitType)).
		SetBillingPeriod(p.BillingPeriod).
		SetBillingPeriodCount(p.BillingPeriodCount).
		SetBillingModel(p.BillingModel).
		SetDisplayName(p.DisplayName).
		SetBillingCadence(p.BillingCadence).
		SetNillableStartDate(p.StartDate).
		SetNillableEndDate(p.EndDate).
		SetNillableMeterID(lo.ToPtr(p.MeterID)).
		SetInvoiceCadence(p.InvoiceCadence).
		SetTrialPeriod(p.TrialPeriod).
		SetNillableTierMode(lo.ToPtr(p.TierMode)).
		SetTiers(p.ToEntTiers()).
		SetTransformQuantity(types.TransformQuantity(p.TransformQuantity)).
		SetLookupKey(p.LookupKey).
		SetDescription(p.Description).
		SetMetadata(map[string]string(p.Metadata)).
		SetStatus(string(p.Status)).
		SetCreatedAt(p.CreatedAt).
		SetUpdatedAt(p.UpdatedAt).
		SetCreatedBy(p.CreatedBy).
		SetUpdatedBy(p.CreatedBy).
		SetEnvironmentID(p.EnvironmentID).
		SetNillableParentPriceID(lo.ToPtr(p.ParentPriceID)).
		SetEntityType(p.EntityType).
		SetEntityID(p.EntityID).
		SetNillableMinQuantity(p.MinQuantity).
		SetNillablePriceUnitID(p.PriceUnitID).
		SetNillablePriceUnit(p.PriceUnit).
		SetNillablePriceUnitAmount(p.PriceUnitAmount).
		SetDisplayPriceUnitAmount(p.DisplayPriceUnitAmount).
		SetNillableConversionRate(p.ConversionRate).
		SetPriceUnitTiers(domainPrice.ToEntTiersFromJSONB(p.PriceUnitTiers))

	price, err := priceBuilder.Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A price with this identifier already exists").
				WithReportableDetails(map[string]any{
					"price_id":   p.ID,
					"lookup_key": p.LookupKey,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create price").
			Mark(ierr.ErrDatabase)
	}

	*p = *domainPrice.FromEnt(price)
	return nil
}

func (r *priceRepository) Get(ctx context.Context, id string) (*domainPrice.Price, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price", "get", map[string]interface{}{
		"price_id":  id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedPrice := r.GetCache(ctx, id); cachedPrice != nil {
		return cachedPrice, nil
	}

	client := r.client.Reader(ctx)

	r.log.Debugw("getting price",
		"price_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	p, err := client.Price.Query().
		Where(
			price.ID(id),
			price.TenantID(types.GetTenantID(ctx)),
			price.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Price with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"price_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get price").
			Mark(ierr.ErrDatabase)
	}

	price := domainPrice.FromEnt(p)
	r.SetCache(ctx, price)
	return price, nil
}

func (r *priceRepository) List(ctx context.Context, filter *types.PriceFilter) ([]*domainPrice.Price, error) {
	if filter == nil {
		filter = &types.PriceFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"filter":    filter,
	})
	defer FinishSpan(span)

	query := client.Price.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	prices, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)

		return nil, ierr.WithError(err).
			WithHint("Failed to list prices").
			Mark(ierr.ErrDatabase)
	}

	return domainPrice.FromEntList(prices), nil
}

func (r *priceRepository) Count(ctx context.Context, filter *types.PriceFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"filter":    filter,
	})
	defer FinishSpan(span)

	query := client.Price.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count prices").
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *priceRepository) ListAll(ctx context.Context, filter *types.PriceFilter) ([]*domainPrice.Price, error) {
	if filter == nil {
		filter = types.NewNoLimitPriceFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	return r.List(ctx, filter)
}

func (r *priceRepository) Update(ctx context.Context, p *domainPrice.Price) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating price",
		"price_id", p.ID,
		"tenant_id", p.TenantID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price", "update", map[string]interface{}{
		"price_id":  p.ID,
		"tenant_id": p.TenantID,
	})
	defer FinishSpan(span)

	_, err := client.Price.Update().
		Where(
			price.ID(p.ID),
			price.TenantID(p.TenantID),
			price.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetAmount(p.Amount).
		SetDisplayAmount(p.DisplayAmount).
		SetType(p.Type).
		SetBillingPeriod((p.BillingPeriod)).
		SetBillingPeriodCount(p.BillingPeriodCount).
		SetBillingModel(p.BillingModel).
		SetBillingCadence(p.BillingCadence).
		SetNillableMeterID(lo.ToPtr(p.MeterID)).
		SetNillableTierMode(lo.ToPtr(p.TierMode)).
		SetTiers(p.ToEntTiers()).
		SetDisplayName(p.DisplayName).
		SetTransformQuantity(types.TransformQuantity(p.TransformQuantity)).
		SetLookupKey(p.LookupKey).
		SetNillableEndDate(p.EndDate).
		SetDescription(p.Description).
		SetMetadata(map[string]string(p.Metadata)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetNillableGroupID(lo.ToPtr(p.GroupID)).
		SetPriceUnitType(string(p.PriceUnitType)).
		SetNillablePriceUnitAmount(p.PriceUnitAmount).
		SetDisplayPriceUnitAmount(p.DisplayPriceUnitAmount).
		SetNillableConversionRate(p.ConversionRate).
		SetPriceUnitTiers(domainPrice.ToEntTiersFromJSONB(p.PriceUnitTiers)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price with ID %s was not found", p.ID).
				WithReportableDetails(map[string]any{
					"price_id": p.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update price").
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, p.ID)
	return nil
}

func (r *priceRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("deleting price",
		"price_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price", "delete", map[string]interface{}{
		"price_id":  id,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	_, err := client.Price.Update().
		Where(
			price.ID(id),
			price.TenantID(types.GetTenantID(ctx)),
			price.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Price with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"price_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete price").
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, id)
	return nil
}

func (r *priceRepository) CreateBulk(ctx context.Context, prices []*domainPrice.Price) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("bulk creating prices",
		"count", len(prices),
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price", "create_bulk", map[string]interface{}{
		"count":     len(prices),
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(prices) == 0 {
		return nil
	}

	builders := make([]*ent.PriceCreate, len(prices))
	for i, p := range prices {

		// Set environment ID from context if not already set
		if p.EnvironmentID == "" {
			p.EnvironmentID = types.GetEnvironmentID(ctx)
		}

		builders[i] = client.Price.Create().
			SetID(p.ID).
			SetTenantID(p.TenantID).
			SetAmount(p.Amount).
			SetCurrency(p.Currency).
			SetDisplayAmount(p.DisplayAmount).
			SetEntityID(p.EntityID).
			SetEntityType(p.EntityType).
			SetType(p.Type).
			SetBillingPeriod(p.BillingPeriod).
			SetBillingPeriodCount(p.BillingPeriodCount).
			SetBillingModel(p.BillingModel).
			SetDisplayName(p.DisplayName).
			SetBillingCadence(p.BillingCadence).
			SetInvoiceCadence(p.InvoiceCadence).
			SetNillableStartDate(p.StartDate).
			SetNillableEndDate(p.EndDate).
			SetTrialPeriod(p.TrialPeriod).
			SetNillableMeterID(lo.ToPtr(p.MeterID)).
			SetNillableTierMode(lo.ToPtr(p.TierMode)).
			SetTiers(p.ToEntTiers()).
			SetTransformQuantity(types.TransformQuantity(p.TransformQuantity)).
			SetLookupKey(p.LookupKey).
			SetDescription(p.Description).
			SetMetadata(map[string]string(p.Metadata)).
			SetEnvironmentID(p.EnvironmentID).
			SetStatus(string(p.Status))
		if p.MinQuantity != nil {
			builders[i] = builders[i].SetMinQuantity(*p.MinQuantity)
		}
		builders[i] = builders[i].
			SetCreatedAt(p.CreatedAt).
			SetUpdatedAt(p.UpdatedAt).
			SetCreatedBy(p.CreatedBy).
			SetUpdatedBy(p.UpdatedBy).
			SetNillableGroupID(lo.ToPtr(p.GroupID)).
			SetPriceUnitType(string(p.PriceUnitType)).
			SetNillablePriceUnitID(p.PriceUnitID).
			SetNillablePriceUnit(p.PriceUnit).
			SetNillablePriceUnitAmount(p.PriceUnitAmount).
			SetDisplayPriceUnitAmount(p.DisplayPriceUnitAmount).
			SetNillableConversionRate(p.ConversionRate).
			SetPriceUnitTiers(domainPrice.ToEntTiersFromJSONB(p.PriceUnitTiers))
	}

	_, err := client.Price.CreateBulk(builders...).Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create prices in bulk").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *priceRepository) DeleteBulk(ctx context.Context, ids []string) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("bulk deleting prices",
		"count", len(ids),
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "price", "delete_bulk", map[string]interface{}{
		"count":     len(ids),
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(ids) == 0 {
		return nil
	}

	_, err := client.Price.Update().
		Where(
			price.IDIn(ids...),
			price.TenantID(types.GetTenantID(ctx)),
			price.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to delete prices in bulk").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// PriceQuery type alias for better readability
type PriceQuery = *ent.PriceQuery

// PriceQueryOptions implements BaseQueryOptions for price queries
type PriceQueryOptions struct{}

func (o PriceQueryOptions) ApplyTenantFilter(ctx context.Context, query PriceQuery) PriceQuery {
	return query.Where(price.TenantID(types.GetTenantID(ctx)))
}

func (o PriceQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query PriceQuery) PriceQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(price.EnvironmentID(environmentID))
	}
	return query
}

func (o PriceQueryOptions) ApplyStatusFilter(query PriceQuery, status string) PriceQuery {
	if status == "" {
		return query.Where(price.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(price.Status(status))
}

func (o PriceQueryOptions) ApplySortFilter(query PriceQuery, field string, order string) PriceQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o PriceQueryOptions) ApplyPaginationFilter(query PriceQuery, limit int, offset int) PriceQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o PriceQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return price.FieldCreatedAt
	case "updated_at":
		return price.FieldUpdatedAt
	case "lookup_key":
		return price.FieldLookupKey
	case "amount":
		return price.FieldAmount
	default:
		return field
	}
}

func (o PriceQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.PriceFilter, query PriceQuery) PriceQuery {
	if f == nil {
		return query
	}

	// Apply price IDs filter if specified
	if len(f.PriceIDs) > 0 {
		query = query.Where(price.IDIn(f.PriceIDs...))
	}

	// entity type filter
	if f.EntityType != nil {
		query = query.Where(price.EntityType(*f.EntityType))
	}

	// entity id filter
	if f.EntityIDs != nil {
		query = query.Where(price.EntityIDIn(f.EntityIDs...))
	}

	// meter id filter
	if len(f.MeterIDs) > 0 {
		query = query.Where(price.MeterIDIn(f.MeterIDs...))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(price.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(price.CreatedAtLTE(*f.EndTime))
		}
	}

	if !f.AllowExpiredPrices {
		now := time.Now().UTC()

		// Filter for active prices:
		// - Start date should be before or equal to current time (or null)
		// - End date should be after current time (or null)
		query = query.Where(
			price.Or(
				price.EndDateIsNil(),
				price.EndDateGT(now),
			),
		)
	}

	// Apply start date less than filter if specified
	if f.StartDateLT != nil {
		query = query.Where(price.StartDateLT(*f.StartDateLT))
	}

	return query
}

func (r *priceRepository) GetByPlanID(ctx context.Context, planID string) ([]*domainPrice.Price, error) {
	client := r.client.Reader(ctx)

	prices, err := client.Price.Query().
		Where(price.EntityID(planID), price.Status(string(types.StatusPublished))).
		All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get prices by plan ID").
			WithReportableDetails(map[string]interface{}{
				"plan_id": planID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return domainPrice.FromEntList(prices), nil
}

func (r *priceRepository) SetCache(ctx context.Context, price *domainPrice.Price) {
	span := cache.StartCacheSpan(ctx, "price", "set", map[string]interface{}{
		"price_id": price.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPrice, tenantID, environmentID, price.ID)
	r.cache.Set(ctx, cacheKey, price, cache.ExpiryDefaultInMemory)
}

func (r *priceRepository) GetCache(ctx context.Context, key string) *domainPrice.Price {
	span := cache.StartCacheSpan(ctx, "price", "get", map[string]interface{}{
		"price_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPrice, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainPrice.Price)
	}
	return nil
}

func (r *priceRepository) DeleteCache(ctx context.Context, priceID string) {
	span := cache.StartCacheSpan(ctx, "price", "delete", map[string]interface{}{
		"price_id": priceID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPrice, tenantID, environmentID, priceID)
	r.cache.Delete(ctx, cacheKey)
}

// Grouping cruds

func (r *priceRepository) GetByGroupIDs(ctx context.Context, groupIDs []string) ([]*domainPrice.Price, error) {
	client := r.client.Reader(ctx)

	prices, err := client.Price.Query().
		Where(price.GroupIDIn(groupIDs...)).
		All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to get prices by group IDs").
			WithReportableDetails(map[string]interface{}{
				"group_ids": groupIDs,
			}).
			Mark(ierr.ErrDatabase)
	}
	return domainPrice.FromEntList(prices), nil
}

func (r *priceRepository) ClearByGroupID(ctx context.Context, groupID string) error {
	client := r.client.Writer(ctx)

	_, err := client.Price.Update().
		Where(
			price.GroupID(groupID),
			price.TenantID(types.GetTenantID(ctx)),
			price.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		ClearGroupID().
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to clear group ID").
			WithReportableDetails(map[string]interface{}{
				"group_id": groupID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}
