package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscriptionlineitem"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionLineItemRepository struct {
	client postgres.IClient
}

// NewSubscriptionLineItemRepository creates a new subscription line item repository
func NewSubscriptionLineItemRepository(client postgres.IClient) subscription.LineItemRepository {
	return &subscriptionLineItemRepository{
		client: client,
	}
}

// Create creates a new subscription line item
func (r *subscriptionLineItemRepository) Create(ctx context.Context, item *subscription.SubscriptionLineItem) error {
	client := r.client.Querier(ctx)
	if client == nil {
		return fmt.Errorf("failed to get client")
	}

	_, err := client.SubscriptionLineItem.Create().
		SetID(item.ID).
		SetSubscriptionID(item.SubscriptionID).
		SetCustomerID(item.CustomerID).
		SetNillablePlanID(types.ToNillableString(item.PlanID)).
		SetNillablePlanDisplayName(types.ToNillableString(item.PlanDisplayName)).
		SetPriceID(item.PriceID).
		SetNillablePriceType(types.ToNillableString(string(item.PriceType))).
		SetNillableMeterID(types.ToNillableString(item.MeterID)).
		SetNillableMeterDisplayName(types.ToNillableString(item.MeterDisplayName)).
		SetNillableDisplayName(types.ToNillableString(item.DisplayName)).
		SetQuantity(item.Quantity).
		SetCurrency(item.Currency).
		SetBillingPeriod(string(item.BillingPeriod)).
		SetNillableStartDate(types.ToNillableTime(item.StartDate)).
		SetNillableEndDate(types.ToNillableTime(item.EndDate)).
		SetMetadata(item.Metadata).
		SetTenantID(item.TenantID).
		SetStatus(string(item.Status)).
		SetCreatedBy(item.CreatedBy).
		SetUpdatedBy(item.UpdatedBy).
		SetCreatedAt(time.Now()).
		SetUpdatedAt(time.Now()).
		Save(ctx)

	return err
}

// Get gets a subscription line item by ID
func (r *subscriptionLineItemRepository) Get(ctx context.Context, id string) (*subscription.SubscriptionLineItem, error) {
	client := r.client.Querier(ctx)
	if client == nil {
		return nil, fmt.Errorf("failed to get client")
	}

	item, err := client.SubscriptionLineItem.Query().
		Where(subscriptionlineitem.ID(id)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.ErrNotFound
		}
		return nil, err
	}

	return subscription.SubscriptionLineItemFromEnt(item), nil
}

// Update updates a subscription line item
func (r *subscriptionLineItemRepository) Update(ctx context.Context, item *subscription.SubscriptionLineItem) error {
	client := r.client.Querier(ctx)
	if client == nil {
		return fmt.Errorf("failed to get client")
	}

	_, err := client.SubscriptionLineItem.UpdateOneID(item.ID).
		SetNillablePlanID(types.ToNillableString(item.PlanID)).
		SetNillablePlanDisplayName(types.ToNillableString(item.PlanDisplayName)).
		SetPriceID(item.PriceID).
		SetNillablePriceType(types.ToNillableString(string(item.PriceType))).
		SetNillableMeterID(types.ToNillableString(item.MeterID)).
		SetNillableMeterDisplayName(types.ToNillableString(item.MeterDisplayName)).
		SetNillableDisplayName(types.ToNillableString(item.DisplayName)).
		SetQuantity(item.Quantity).
		SetCurrency(item.Currency).
		SetBillingPeriod(string(item.BillingPeriod)).
		SetNillableStartDate(types.ToNillableTime(item.StartDate)).
		SetNillableEndDate(types.ToNillableTime(item.EndDate)).
		SetMetadata(item.Metadata).
		SetStatus(string(item.Status)).
		SetUpdatedBy(item.UpdatedBy).
		SetUpdatedAt(time.Now()).
		Save(ctx)

	return err
}

// Delete deletes a subscription line item
func (r *subscriptionLineItemRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)
	if client == nil {
		return fmt.Errorf("failed to get client")
	}

	return client.SubscriptionLineItem.DeleteOneID(id).Exec(ctx)
}

// CreateBulk creates multiple subscription line items
func (r *subscriptionLineItemRepository) CreateBulk(ctx context.Context, items []*subscription.SubscriptionLineItem) error {
	client := r.client.Querier(ctx)
	if client == nil {
		return fmt.Errorf("failed to get client")
	}

	bulk := make([]*ent.SubscriptionLineItemCreate, len(items))
	for i, item := range items {
		bulk[i] = client.SubscriptionLineItem.Create().
			SetID(item.ID).
			SetSubscriptionID(item.SubscriptionID).
			SetCustomerID(item.CustomerID).
			SetNillablePlanID(types.ToNillableString(item.PlanID)).
			SetNillablePlanDisplayName(types.ToNillableString(item.PlanDisplayName)).
			SetPriceID(item.PriceID).
			SetNillablePriceType(types.ToNillableString(string(item.PriceType))).
			SetNillableMeterID(types.ToNillableString(item.MeterID)).
			SetNillableMeterDisplayName(types.ToNillableString(item.MeterDisplayName)).
			SetNillableDisplayName(types.ToNillableString(item.DisplayName)).
			SetQuantity(item.Quantity).
			SetCurrency(item.Currency).
			SetBillingPeriod(string(item.BillingPeriod)).
			SetNillableStartDate(types.ToNillableTime(item.StartDate)).
			SetNillableEndDate(types.ToNillableTime(item.EndDate)).
			SetMetadata(item.Metadata).
			SetTenantID(item.TenantID).
			SetStatus(string(item.Status)).
			SetCreatedBy(item.CreatedBy).
			SetUpdatedBy(item.UpdatedBy).
			SetCreatedAt(time.Now()).
			SetUpdatedAt(time.Now())
	}

	return client.SubscriptionLineItem.CreateBulk(bulk...).Exec(ctx)
}

// ListBySubscription lists all subscription line items for a subscription
func (r *subscriptionLineItemRepository) ListBySubscription(ctx context.Context, subscriptionID string) ([]*subscription.SubscriptionLineItem, error) {
	client := r.client.Querier(ctx)
	if client == nil {
		return nil, fmt.Errorf("failed to get client")
	}

	items, err := client.SubscriptionLineItem.Query().
		Where(subscriptionlineitem.SubscriptionID(subscriptionID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	return subscription.GetLineItemFromEntList(items), nil
}

// ListByCustomer lists all subscription line items for a customer
func (r *subscriptionLineItemRepository) ListByCustomer(ctx context.Context, customerID string) ([]*subscription.SubscriptionLineItem, error) {
	client := r.client.Querier(ctx)
	if client == nil {
		return nil, fmt.Errorf("failed to get client")
	}

	items, err := client.SubscriptionLineItem.Query().
		Where(subscriptionlineitem.CustomerID(customerID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	return subscription.GetLineItemFromEntList(items), nil
}

// List lists subscription line items based on filter
func (r *subscriptionLineItemRepository) List(ctx context.Context, filter *types.SubscriptionLineItemFilter) ([]*subscription.SubscriptionLineItem, error) {
	client := r.client.Querier(ctx)
	if client == nil {
		return nil, fmt.Errorf("failed to get client")
	}

	query := client.SubscriptionLineItem.Query()

	if len(filter.SubscriptionIDs) > 0 {
		query = query.Where(subscriptionlineitem.SubscriptionIDIn(filter.SubscriptionIDs...))
	}
	if len(filter.CustomerIDs) > 0 {
		query = query.Where(subscriptionlineitem.CustomerIDIn(filter.CustomerIDs...))
	}
	if len(filter.PlanIDs) > 0 {
		query = query.Where(subscriptionlineitem.PlanIDIn(filter.PlanIDs...))
	}
	if len(filter.PriceIDs) > 0 {
		query = query.Where(subscriptionlineitem.PriceIDIn(filter.PriceIDs...))
	}
	if len(filter.MeterIDs) > 0 {
		query = query.Where(subscriptionlineitem.MeterIDIn(filter.MeterIDs...))
	}
	if len(filter.Currencies) > 0 {
		query = query.Where(subscriptionlineitem.CurrencyIn(filter.Currencies...))
	}
	if len(filter.BillingPeriods) > 0 {
		query = query.Where(subscriptionlineitem.BillingPeriodIn(filter.BillingPeriods...))
	}

	items, err := query.
		Limit(filter.GetLimit()).
		Offset(filter.GetOffset()).
		Order(ent.Desc(subscriptionlineitem.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	return subscription.GetLineItemFromEntList(items), nil
}

// Count counts subscription line items based on filter
func (r *subscriptionLineItemRepository) Count(ctx context.Context, filter *types.SubscriptionLineItemFilter) (int, error) {
	client := r.client.Querier(ctx)
	if client == nil {
		return 0, fmt.Errorf("failed to get client")
	}

	query := client.SubscriptionLineItem.Query()

	if len(filter.SubscriptionIDs) > 0 {
		query = query.Where(subscriptionlineitem.SubscriptionIDIn(filter.SubscriptionIDs...))
	}
	if len(filter.CustomerIDs) > 0 {
		query = query.Where(subscriptionlineitem.CustomerIDIn(filter.CustomerIDs...))
	}
	if len(filter.PlanIDs) > 0 {
		query = query.Where(subscriptionlineitem.PlanIDIn(filter.PlanIDs...))
	}
	if len(filter.PriceIDs) > 0 {
		query = query.Where(subscriptionlineitem.PriceIDIn(filter.PriceIDs...))
	}
	if len(filter.MeterIDs) > 0 {
		query = query.Where(subscriptionlineitem.MeterIDIn(filter.MeterIDs...))
	}
	if len(filter.Currencies) > 0 {
		query = query.Where(subscriptionlineitem.CurrencyIn(filter.Currencies...))
	}
	if len(filter.BillingPeriods) > 0 {
		query = query.Where(subscriptionlineitem.BillingPeriodIn(filter.BillingPeriods...))
	}

	return query.Count(ctx)
}

// GetByPriceID gets subscription line items by price ID
func (r *subscriptionLineItemRepository) GetByPriceID(ctx context.Context, priceID string) ([]*subscription.SubscriptionLineItem, error) {
	client := r.client.Querier(ctx)
	if client == nil {
		return nil, fmt.Errorf("failed to get client")
	}

	items, err := client.SubscriptionLineItem.Query().
		Where(subscriptionlineitem.PriceID(priceID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	return subscription.GetLineItemFromEntList(items), nil
}

// GetByPlanID gets subscription line items by plan ID
func (r *subscriptionLineItemRepository) GetByPlanID(ctx context.Context, planID string) ([]*subscription.SubscriptionLineItem, error) {
	client := r.client.Querier(ctx)
	if client == nil {
		return nil, fmt.Errorf("failed to get client")
	}

	items, err := client.SubscriptionLineItem.Query().
		Where(subscriptionlineitem.PlanID(planID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	return subscription.GetLineItemFromEntList(items), nil
}
