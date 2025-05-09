package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscriptionlineitem"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "create", map[string]interface{}{
		"subscription_id": item.SubscriptionID,
		"price_id":        item.PriceID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if item.EnvironmentID == "" {
		item.EnvironmentID = types.GetEnvironmentID(ctx)
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
		SetInvoiceCadence(string(item.InvoiceCadence)).
		SetTrialPeriod(item.TrialPeriod).
		SetMetadata(item.Metadata).
		SetTenantID(item.TenantID).
		SetEnvironmentID(item.EnvironmentID).
		SetStatus(string(item.Status)).
		SetCreatedBy(item.CreatedBy).
		SetUpdatedBy(item.UpdatedBy).
		SetCreatedAt(item.CreatedAt).
		SetUpdatedAt(item.UpdatedAt).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create subscription line item").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": item.SubscriptionID,
				"price_id":        item.PriceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// Get retrieves a subscription line item by ID
func (r *subscriptionLineItemRepository) Get(ctx context.Context, id string) (*subscription.SubscriptionLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "get", map[string]interface{}{
		"line_item_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	if client == nil {
		err := ierr.NewError("failed to get database client").
			WithHint("Database client is not available").
			Mark(ierr.ErrDatabase)
		SetSpanError(span, err)
		return nil, err
	}

	item, err := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.ID(id),
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Subscription line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return subscription.SubscriptionLineItemFromEnt(item), nil
}

// Update updates a subscription line item
func (r *subscriptionLineItemRepository) Update(ctx context.Context, item *subscription.SubscriptionLineItem) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "update", map[string]interface{}{
		"line_item_id": item.ID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
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

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription line item not found").
				WithReportableDetails(map[string]interface{}{
					"line_item_id": item.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": item.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// Delete deletes a subscription line item
func (r *subscriptionLineItemRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "delete", map[string]interface{}{
		"line_item_id": id,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	_, err := client.SubscriptionLineItem.Delete().
		Where(
			subscriptionlineitem.ID(id),
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
		).
		Exec(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete subscription line item").
			WithReportableDetails(map[string]interface{}{
				"line_item_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// CreateBulk creates multiple subscription line items in bulk
func (r *subscriptionLineItemRepository) CreateBulk(ctx context.Context, items []*subscription.SubscriptionLineItem) error {
	if len(items) == 0 {
		return nil
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "create_bulk", map[string]interface{}{
		"item_count": len(items),
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	// Create bulk operation
	bulk := make([]*ent.SubscriptionLineItemCreate, len(items))
	for i, item := range items {
		if item.EnvironmentID == "" {
			item.EnvironmentID = types.GetEnvironmentID(ctx)
		}

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
			SetInvoiceCadence(string(item.InvoiceCadence)).
			SetTrialPeriod(item.TrialPeriod).
			SetNillableStartDate(types.ToNillableTime(item.StartDate)).
			SetNillableEndDate(types.ToNillableTime(item.EndDate)).
			SetMetadata(item.Metadata).
			SetTenantID(item.TenantID).
			SetEnvironmentID(item.EnvironmentID).
			SetStatus(string(item.Status)).
			SetCreatedBy(item.CreatedBy).
			SetUpdatedBy(item.UpdatedBy).
			SetCreatedAt(item.CreatedAt).
			SetUpdatedAt(item.UpdatedAt)
	}

	// Execute bulk create
	_, err := client.SubscriptionLineItem.CreateBulk(bulk...).Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to create subscription line items in bulk").
			WithReportableDetails(map[string]interface{}{
				"count": len(items),
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// ListBySubscription retrieves all line items for a subscription
func (r *subscriptionLineItemRepository) ListBySubscription(ctx context.Context, subscriptionID string) ([]*subscription.SubscriptionLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "list_by_subscription", map[string]interface{}{
		"subscription_id": subscriptionID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	items, err := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.SubscriptionID(subscriptionID),
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription line items").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": subscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return subscription.GetLineItemFromEntList(items), nil
}

// ListByCustomer retrieves all line items for a customer
func (r *subscriptionLineItemRepository) ListByCustomer(ctx context.Context, customerID string) ([]*subscription.SubscriptionLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "list_by_customer", map[string]interface{}{
		"customer_id": customerID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	items, err := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.CustomerID(customerID),
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list customer subscription line items").
			WithReportableDetails(map[string]interface{}{
				"customer_id": customerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return subscription.GetLineItemFromEntList(items), nil
}

// List retrieves subscription line items based on filter
func (r *subscriptionLineItemRepository) List(ctx context.Context, filter *types.SubscriptionLineItemFilter) ([]*subscription.SubscriptionLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	if client == nil {
		err := ierr.NewError("failed to get database client").
			WithHint("Database client is not available").
			Mark(ierr.ErrDatabase)
		SetSpanError(span, err)
		return nil, err
	}

	query := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	// Apply filters
	if filter != nil {
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

		// Apply pagination
		if filter.Limit != nil {
			query = query.Limit(*filter.Limit)
		}
		if filter.Offset != nil {
			query = query.Offset(*filter.Offset)
		}
	}

	items, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription line items").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return subscription.GetLineItemFromEntList(items), nil
}

// Count counts subscription line items based on filter
func (r *subscriptionLineItemRepository) Count(ctx context.Context, filter *types.SubscriptionLineItemFilter) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	if client == nil {
		err := ierr.NewError("failed to get database client").
			WithHint("Database client is not available").
			Mark(ierr.ErrDatabase)
		SetSpanError(span, err)
		return 0, err
	}

	query := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	// Apply filters
	if filter != nil {
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
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count subscription line items").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

// GetByPriceID retrieves all line items for a price
func (r *subscriptionLineItemRepository) GetByPriceID(ctx context.Context, priceID string) ([]*subscription.SubscriptionLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "get_by_price_id", map[string]interface{}{
		"price_id": priceID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	items, err := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.PriceID(priceID),
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription line items by price").
			WithReportableDetails(map[string]interface{}{
				"price_id": priceID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return subscription.GetLineItemFromEntList(items), nil
}

// GetByPlanID retrieves all line items for a plan
func (r *subscriptionLineItemRepository) GetByPlanID(ctx context.Context, planID string) ([]*subscription.SubscriptionLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "get_by_plan_id", map[string]interface{}{
		"plan_id": planID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)
	if client == nil {
		err := ierr.NewError("failed to get database client").
			WithHint("Database client is not available").
			Mark(ierr.ErrDatabase)
		SetSpanError(span, err)
		return nil, err
	}

	items, err := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.PlanID(planID),
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription line items by plan").
			WithReportableDetails(map[string]interface{}{
				"plan_id": planID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return subscription.GetLineItemFromEntList(items), nil
}
