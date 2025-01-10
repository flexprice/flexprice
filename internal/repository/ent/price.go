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
	client postgres.IClient
	log    *logger.Logger
}

func NewPriceRepository(client postgres.IClient, log *logger.Logger) domainPrice.Repository {
	return &priceRepository{
		client: client,
		log:    log,
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

	r.log.Debug("getting price",
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

func (r *priceRepository) List(ctx context.Context, filter types.PriceFilter) ([]*domainPrice.Price, error) {
	client := r.client.Querier(ctx)

	r.log.Debug("listing prices",
		"tenant_id", types.GetTenantID(ctx),
		"plan_ids", filter.PlanIDs,
		"limit", filter.Limit,
		"offset", filter.Offset,
	)

	query := client.Price.Query().
		Where(price.TenantID(types.GetTenantID(ctx)))

	if len(filter.PlanIDs) > 0 {
		query = query.Where(price.PlanIDIn(filter.PlanIDs...))
	}

	prices, err := query.
		Order(ent.Desc(price.FieldCreatedAt)).
		Limit(lo.FromPtr(filter.Limit)).
		Offset(lo.FromPtr(filter.Offset)).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to list prices: %w", err)
	}

	return domainPrice.FromEntList(prices), nil
}

func (r *priceRepository) Update(ctx context.Context, p *domainPrice.Price) error {
	client := r.client.Querier(ctx)

	r.log.Debug("updating price",
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

	r.log.Debug("deleting price",
		"price_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Price.Update().
		Where(
			price.ID(id),
			price.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
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
