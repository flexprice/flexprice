package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/price"
	"github.com/flexprice/flexprice/ent/schema"
	domainPrice "github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type priceRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts PriceQueryOptions
}

func NewPriceRepository(client postgres.IClient, log *logger.Logger) domainPrice.Repository {
	return &priceRepository{
		client:    client,
		log:       log,
		queryOpts: PriceQueryOptions{},
	}
}

func (r *priceRepository) Create(ctx context.Context, p *domainPrice.Price) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating price",
		"price_id", p.ID,
		"tenant_id", p.TenantID,
		"plan_id", p.PlanID,
		"lookup_key", p.LookupKey,
	)

	price, err := client.Price.Create().
		SetID(p.ID).
		SetTenantID(p.TenantID).
		SetAmount(p.Amount.InexactFloat64()).
		SetCurrency(p.Currency).
		SetDisplayAmount(p.DisplayAmount).
		SetPlanID(p.PlanID).
		SetType(string(p.Type)).
		SetBillingPeriod(string(p.BillingPeriod)).
		SetBillingPeriodCount(p.BillingPeriodCount).
		SetBillingModel(string(p.BillingModel)).
		SetBillingCadence(string(p.BillingCadence)).
		SetNillableMeterID(lo.ToPtr(p.MeterID)).
		SetFilterValues(map[string][]string(p.FilterValues)).
		SetNillableTierMode(lo.ToPtr(string(p.TierMode))).
		SetTiers(p.ToEntTiers()).
		SetTransformQuantity(schema.TransformQuantity(p.TransformQuantity)).
		SetLookupKey(p.LookupKey).
		SetDescription(p.Description).
		SetMetadata(map[string]string(p.Metadata)).
		SetStatus(string(p.Status)).
		SetCreatedAt(p.CreatedAt).
		SetUpdatedAt(p.UpdatedAt).
		SetCreatedBy(p.CreatedBy).
		SetUpdatedBy(p.UpdatedBy).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create price: %w", err)
	}

	*p = *domainPrice.FromEnt(price)
	return nil
}

func (r *priceRepository) Get(ctx context.Context, id string) (*domainPrice.Price, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting price",
		"price_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	p, err := client.Price.Query().
		Where(
			price.ID(id),
			price.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("price not found")
		}
		return nil, fmt.Errorf("failed to get price: %w", err)
	}

	return domainPrice.FromEnt(p), nil
}

func (r *priceRepository) List(ctx context.Context, filter *types.PriceFilter) ([]*domainPrice.Price, error) {
	if filter == nil {
		filter = &types.PriceFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Querier(ctx)
	query := client.Price.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	prices, err := query.All(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list prices: %w", err)
	}

	return domainPrice.FromEntList(prices), nil
}

func (r *priceRepository) Count(ctx context.Context, filter *types.PriceFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Price.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count prices: %w", err)
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
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	return r.List(ctx, filter)
}

func (r *priceRepository) Update(ctx context.Context, p *domainPrice.Price) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating price",
		"price_id", p.ID,
		"tenant_id", p.TenantID,
	)

	_, err := client.Price.Update().
		Where(
			price.ID(p.ID),
			price.TenantID(p.TenantID),
		).
		SetAmount(p.Amount.InexactFloat64()).
		SetDisplayAmount(p.DisplayAmount).
		SetType(string(p.Type)).
		SetBillingPeriod(string(p.BillingPeriod)).
		SetBillingPeriodCount(p.BillingPeriodCount).
		SetBillingModel(string(p.BillingModel)).
		SetBillingCadence(string(p.BillingCadence)).
		SetNillableMeterID(lo.ToPtr(p.MeterID)).
		SetFilterValues(map[string][]string(p.FilterValues)).
		SetNillableTierMode(lo.ToPtr(string(p.TierMode))).
		SetTiers(p.ToEntTiers()).
		SetTransformQuantity(schema.TransformQuantity(p.TransformQuantity)).
		SetLookupKey(p.LookupKey).
		SetDescription(p.Description).
		SetMetadata(map[string]string(p.Metadata)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("price not found")
		}
		return fmt.Errorf("failed to update price: %w", err)
	}

	return nil
}

func (r *priceRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting price",
		"price_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Price.Update().
		Where(
			price.ID(id),
			price.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("price not found")
		}
		return fmt.Errorf("failed to delete price: %w", err)
	}

	return nil
}

func (r *priceRepository) CreateBulk(ctx context.Context, prices []*domainPrice.Price) error {
	if len(prices) == 0 {
		return nil
	}

	r.log.Debugw("creating prices in bulk", "count", len(prices))

	builders := make([]*ent.PriceCreate, len(prices))
	for i, p := range prices {
		builders[i] = r.client.Querier(ctx).Price.Create().
			SetID(p.ID).
			SetTenantID(p.TenantID).
			SetAmount(p.Amount.InexactFloat64()).
			SetCurrency(p.Currency).
			SetDisplayAmount(p.DisplayAmount).
			SetPlanID(p.PlanID).
			SetType(string(p.Type)).
			SetBillingPeriod(string(p.BillingPeriod)).
			SetBillingPeriodCount(p.BillingPeriodCount).
			SetBillingModel(string(p.BillingModel)).
			SetBillingCadence(string(p.BillingCadence)).
			SetNillableMeterID(lo.ToPtr(p.MeterID)).
			SetFilterValues(map[string][]string(p.FilterValues)).
			SetNillableTierMode(lo.ToPtr(string(p.TierMode))).
			SetTiers(p.ToEntTiers()).
			SetTransformQuantity(schema.TransformQuantity(p.TransformQuantity)).
			SetLookupKey(p.LookupKey).
			SetDescription(p.Description).
			SetMetadata(map[string]string(p.Metadata)).
			SetStatus(string(p.Status)).
			SetCreatedAt(p.CreatedAt).
			SetUpdatedAt(p.UpdatedAt).
			SetCreatedBy(p.CreatedBy).
			SetUpdatedBy(p.UpdatedBy)
	}

	_, err := r.client.Querier(ctx).Price.CreateBulk(builders...).Save(ctx)
	if err != nil {
		return fmt.Errorf("failed to create prices in bulk: %w", err)
	}

	return nil
}

func (r *priceRepository) DeleteBulk(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	r.log.Debugw("deleting prices in bulk", "count", len(ids))

	_, err := r.client.Querier(ctx).Price.Update().
		Where(
			price.IDIn(ids...),
			price.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to delete prices in bulk: %w", err)
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

func (o PriceQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.PriceFilter, query PriceQuery) PriceQuery {
	if f == nil {
		return query
	}

	// Apply plan IDs filter if specified
	if len(f.PlanIDs) > 0 {
		query = query.Where(price.PlanIDIn(f.PlanIDs...))
	}

	// Apply price IDs filter if specified
	if len(f.PriceIDs) > 0 {
		query = query.Where(price.IDIn(f.PriceIDs...))
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

	return query
}
