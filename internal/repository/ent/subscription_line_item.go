package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscriptionlineitem"
	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionLineItemRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts SubscriptionLineItemQueryOptions
	cache     cache.Cache
}

// NewSubscriptionLineItemRepository creates a new subscription line item repository
func NewSubscriptionLineItemRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) subscription.LineItemRepository {
	return &subscriptionLineItemRepository{
		client:    client,
		log:       log,
		queryOpts: SubscriptionLineItemQueryOptions{},
		cache:     cache,
	}
}

// applyActiveLineItemFilter applies the filter to ensure only active subscription line items are returned
// Active line items are those where EndDate > currentPeriodStart or EndDate is nil
func (o *SubscriptionLineItemQueryOptions) applyActiveLineItemFilter(query *ent.SubscriptionLineItemQuery, currentPeriodStart *time.Time) *ent.SubscriptionLineItemQuery {
	if currentPeriodStart == nil {
		return query
	}

	return query.Where(
		subscriptionlineitem.Status(string(types.StatusPublished)),
		subscriptionlineitem.Or(
			subscriptionlineitem.EndDateGT(*currentPeriodStart),
			subscriptionlineitem.EndDateIsNil(),
		),
	)
}

// Create creates a new subscription line item
func (r *subscriptionLineItemRepository) Create(ctx context.Context, item *subscription.SubscriptionLineItem) error {
	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "create", map[string]interface{}{
		"subscription_id": item.SubscriptionID,
		"price_id":        item.PriceID,
		"tenant_id":       item.TenantID,
	})
	defer FinishSpan(span)

	r.log.Debugw("creating subscription line item",
		"line_item_id", item.ID,
		"subscription_id", item.SubscriptionID,
		"price_id", item.PriceID,
		"tenant_id", item.TenantID,
	)

	// Set environment ID from context if not already set
	if item.EnvironmentID == "" {
		item.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.SubscriptionLineItem.Create().
		SetID(item.ID).
		SetSubscriptionID(item.SubscriptionID).
		SetCustomerID(item.CustomerID).
		SetNillableEntityID(types.ToNillableString(item.EntityID)).
		SetNillableEntityType(func() *types.InvoiceLineItemEntityType {
			if item.EntityType == "" {
				return nil
			}
			t := types.InvoiceLineItemEntityType(item.EntityType)
			return &t
		}()).
		SetNillablePlanDisplayName(types.ToNillableString(item.PlanDisplayName)).
		SetPriceID(item.PriceID).
		SetNillablePriceType(func() *types.PriceType {
			if item.PriceType == "" {
				return nil
			}
			t := types.PriceType(item.PriceType)
			return &t
		}()).
		SetNillableMeterID(types.ToNillableString(item.MeterID)).
		SetNillableMeterDisplayName(types.ToNillableString(item.MeterDisplayName)).
		SetNillablePriceUnitID(types.ToNillableString(item.PriceUnitID)).
		SetNillablePriceUnit(types.ToNillableString(item.PriceUnit)).
		SetNillableDisplayName(types.ToNillableString(item.DisplayName)).
		SetQuantity(item.Quantity).
		SetCurrency(item.Currency).
		SetBillingPeriod(item.BillingPeriod).
		SetNillableStartDate(types.ToNillableTime(item.StartDate)).
		SetNillableEndDate(types.ToNillableTime(item.EndDate)).
		SetNillableSubscriptionPhaseID(item.SubscriptionPhaseID).
		SetInvoiceCadence(item.InvoiceCadence).
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
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHintf("A subscription line item with ID %s already exists", item.ID).
				WithReportableDetails(map[string]interface{}{
					"line_item_id":    item.ID,
					"subscription_id": item.SubscriptionID,
					"price_id":        item.PriceID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
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
		"tenant_id":    types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedItem := r.GetCache(ctx, id); cachedItem != nil {
		return cachedItem, nil
	}

	client := r.client.Reader(ctx)
	if client == nil {
		err := ierr.NewError("failed to get database client").
			WithHint("Database client is not available").
			Mark(ierr.ErrDatabase)
		SetSpanError(span, err)
		return nil, err
	}

	r.log.Debugw("getting subscription line item",
		"line_item_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	item, err := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.ID(id),
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Subscription line item with ID %s not found", id).
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

	lineItemData := subscription.SubscriptionLineItemFromEnt(item)
	r.SetCache(ctx, lineItemData)
	SetSpanSuccess(span)
	return lineItemData, nil
}

// Update updates a subscription line item
func (r *subscriptionLineItemRepository) Update(ctx context.Context, item *subscription.SubscriptionLineItem) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "update", map[string]interface{}{
		"line_item_id": item.ID,
		"tenant_id":    item.TenantID,
	})
	defer FinishSpan(span)

	r.log.Debugw("updating subscription line item",
		"line_item_id", item.ID,
		"tenant_id", item.TenantID,
	)

	client := r.client.Writer(ctx)
	_, err := client.SubscriptionLineItem.UpdateOneID(item.ID).
		SetNillableEntityID(types.ToNillableString(item.EntityID)).
		SetNillablePlanDisplayName(types.ToNillableString(item.PlanDisplayName)).
		SetPriceID(item.PriceID).
		SetNillablePriceType(func() *types.PriceType {
			if item.PriceType == "" {
				return nil
			}
			t := types.PriceType(item.PriceType)
			return &t
		}()).
		SetNillableMeterID(types.ToNillableString(item.MeterID)).
		SetNillableMeterDisplayName(types.ToNillableString(item.MeterDisplayName)).
		SetNillablePriceUnitID(types.ToNillableString(item.PriceUnitID)).
		SetNillablePriceUnit(types.ToNillableString(item.PriceUnit)).
		SetNillableDisplayName(types.ToNillableString(item.DisplayName)).
		SetQuantity(item.Quantity).
		SetCurrency(item.Currency).
		SetBillingPeriod(item.BillingPeriod).
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

	// Invalidate cache after update
	r.DeleteCache(ctx, item.ID)
	SetSpanSuccess(span)
	return nil
}

// Delete deletes a subscription line item
func (r *subscriptionLineItemRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "delete", map[string]interface{}{
		"line_item_id": id,
		"tenant_id":    types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	r.log.Debugw("deleting subscription line item",
		"line_item_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	client := r.client.Writer(ctx)
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

	// Invalidate cache after delete
	r.DeleteCache(ctx, id)
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

	r.log.Debugw("creating subscription line items in bulk",
		"item_count", len(items),
		"tenant_id", types.GetTenantID(ctx),
	)

	client := r.client.Writer(ctx)

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
			SetNillableEntityID(types.ToNillableString(item.EntityID)).
			SetNillableEntityType(func() *types.InvoiceLineItemEntityType {
				if item.EntityType == "" {
					return nil
				}
				t := types.InvoiceLineItemEntityType(item.EntityType)
				return &t
			}()).
			SetNillablePlanDisplayName(types.ToNillableString(item.PlanDisplayName)).
			SetPriceID(item.PriceID).
			SetNillablePriceType(func() *types.PriceType {
				if item.PriceType == "" {
					return nil
				}
				t := types.PriceType(item.PriceType)
				return &t
			}()).
			SetNillableMeterID(types.ToNillableString(item.MeterID)).
			SetNillableMeterDisplayName(types.ToNillableString(item.MeterDisplayName)).
			SetNillablePriceUnitID(types.ToNillableString(item.PriceUnitID)).
			SetNillablePriceUnit(types.ToNillableString(item.PriceUnit)).
			SetNillableDisplayName(types.ToNillableString(item.DisplayName)).
			SetQuantity(item.Quantity).
			SetCurrency(item.Currency).
			SetBillingPeriod(item.BillingPeriod).
			SetInvoiceCadence(item.InvoiceCadence).
			SetTrialPeriod(item.TrialPeriod).
			SetNillableStartDate(types.ToNillableTime(item.StartDate)).
			SetNillableEndDate(types.ToNillableTime(item.EndDate)).
			SetNillableSubscriptionPhaseID(item.SubscriptionPhaseID).
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

// ListBySubscription retrieves all line items for a subscription.
// This is the source of truth for fetching subscription line items and should be used
// whenever possible instead of implementing custom line item queries. This ensures
// consistent behavior across the codebase, including proper caching and filtering.
func (r *subscriptionLineItemRepository) ListBySubscription(ctx context.Context, sub *subscription.Subscription) ([]*subscription.SubscriptionLineItem, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "list_by_subscription", map[string]interface{}{
		"subscription_id": sub.ID,
		"tenant_id":       types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	r.log.Debugw("listing subscription line items by subscription",
		"subscription_id", sub.ID,
		"tenant_id", types.GetTenantID(ctx),
	)

	client := r.client.Reader(ctx)

	query := client.SubscriptionLineItem.Query().
		Where(
			subscriptionlineitem.SubscriptionID(sub.ID),
			subscriptionlineitem.TenantID(types.GetTenantID(ctx)),
			subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)),
		)

	query = r.queryOpts.applyActiveLineItemFilter(query, &sub.CurrentPeriodStart)

	items, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription line items").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return subscription.GetLineItemFromEntList(items), nil
}

// List retrieves subscription line items based on filter
func (r *subscriptionLineItemRepository) List(ctx context.Context, filter *types.SubscriptionLineItemFilter) ([]*subscription.SubscriptionLineItem, error) {
	if filter == nil {
		filter = &types.SubscriptionLineItemFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	client := r.client.Reader(ctx)
	if client == nil {
		err := ierr.NewError("failed to get database client").
			WithHint("Database client is not available").
			Mark(ierr.ErrDatabase)
		return nil, err
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "list", map[string]interface{}{
		"tenant_id":        types.GetTenantID(ctx),
		"subscription_ids": filter.SubscriptionIDs,
		"entity_ids":       filter.EntityIDs,
		"price_ids":        filter.PriceIDs,
	})
	defer FinishSpan(span)

	query := client.SubscriptionLineItem.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter.QueryFilter, r.queryOpts)

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
	if filter == nil {
		filter = &types.SubscriptionLineItemFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	if err := filter.Validate(); err != nil {
		return 0, ierr.WithError(err).
			WithHint("Invalid filter parameters").
			Mark(ierr.ErrValidation)
	}

	client := r.client.Reader(ctx)
	if client == nil {
		err := ierr.NewError("failed to get database client").
			WithHint("Database client is not available").
			Mark(ierr.ErrDatabase)
		return 0, err
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "subscription_line_item", "count", map[string]interface{}{
		"tenant_id":        types.GetTenantID(ctx),
		"subscription_ids": filter.SubscriptionIDs,
		"entity_ids":       filter.EntityIDs,
		"price_ids":        filter.PriceIDs,
	})
	defer FinishSpan(span)

	query := client.SubscriptionLineItem.Query()

	// Apply entity-specific filters
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter.QueryFilter, r.queryOpts)

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

// SubscriptionLineItemQuery type alias for better readability
type SubscriptionLineItemQuery = *ent.SubscriptionLineItemQuery

// SubscriptionLineItemQueryOptions implements BaseQueryOptions for subscription line item queries
type SubscriptionLineItemQueryOptions struct{}

func (o SubscriptionLineItemQueryOptions) ApplyTenantFilter(ctx context.Context, query SubscriptionLineItemQuery) SubscriptionLineItemQuery {
	return query.Where(subscriptionlineitem.TenantID(types.GetTenantID(ctx)))
}

func (o SubscriptionLineItemQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query SubscriptionLineItemQuery) SubscriptionLineItemQuery {
	return query.Where(subscriptionlineitem.EnvironmentID(types.GetEnvironmentID(ctx)))
}

func (o SubscriptionLineItemQueryOptions) ApplyStatusFilter(query SubscriptionLineItemQuery, status string) SubscriptionLineItemQuery {
	if status != "" {
		return query.Where(subscriptionlineitem.Status(status))
	}
	return query
}

func (o SubscriptionLineItemQueryOptions) ApplySortFilter(query SubscriptionLineItemQuery, field string, order string) SubscriptionLineItemQuery {
	if field != "" {
		if order == "desc" {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o SubscriptionLineItemQueryOptions) ApplyPaginationFilter(query SubscriptionLineItemQuery, limit int, offset int) SubscriptionLineItemQuery {
	return query.Limit(limit).Offset(offset)
}

func (o SubscriptionLineItemQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return subscriptionlineitem.FieldCreatedAt
	case "updated_at":
		return subscriptionlineitem.FieldUpdatedAt
	case "start_date":
		return subscriptionlineitem.FieldStartDate
	case "end_date":
		return subscriptionlineitem.FieldEndDate
	case "status":
		return subscriptionlineitem.FieldStatus
	case "subscription_id":
		return subscriptionlineitem.FieldSubscriptionID
	case "price_id":
		return subscriptionlineitem.FieldPriceID
	case "entity_id":
		return subscriptionlineitem.FieldEntityID
	case "entity_type":
		return subscriptionlineitem.FieldEntityType
	case "meter_id":
		return subscriptionlineitem.FieldMeterID
	case "currency":
		return subscriptionlineitem.FieldCurrency
	case "billing_period":
		return subscriptionlineitem.FieldBillingPeriod
	default:
		return field
	}
}

// applyEntityQueryOptions applies subscription line item-specific filters to the query
func (o *SubscriptionLineItemQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.SubscriptionLineItemFilter, query SubscriptionLineItemQuery) (SubscriptionLineItemQuery, error) {
	// Apply subscription IDs filter if specified
	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(subscriptionlineitem.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	// Apply entity IDs filter if specified
	if len(f.EntityIDs) > 0 {
		query = query.Where(subscriptionlineitem.EntityIDIn(f.EntityIDs...))
	}
	if f.EntityType != nil {
		query = query.Where(subscriptionlineitem.EntityType(types.InvoiceLineItemEntityType(*f.EntityType)))
	}

	// Apply price IDs filter if specified
	if len(f.PriceIDs) > 0 {
		query = query.Where(subscriptionlineitem.PriceIDIn(f.PriceIDs...))
	}
	if len(f.MeterIDs) > 0 {
		query = query.Where(subscriptionlineitem.MeterIDIn(f.MeterIDs...))
	}
	if len(f.Currencies) > 0 {
		query = query.Where(subscriptionlineitem.CurrencyIn(f.Currencies...))
	}
	if len(f.BillingPeriods) > 0 {
		periods := make([]types.BillingPeriod, len(f.BillingPeriods))
		for i, p := range f.BillingPeriods {
			periods[i] = types.BillingPeriod(p)
		}
		query = query.Where(subscriptionlineitem.BillingPeriodIn(periods...))
	}

	if f.ActiveFilter {
		query = o.applyActiveLineItemFilter(query, f.CurrentPeriodStart)
	}

	return query, nil
}

// Cache operations
func (r *subscriptionLineItemRepository) SetCache(ctx context.Context, lineItem *subscription.SubscriptionLineItem) {
	span := cache.StartCacheSpan(ctx, "subscription_line_item", "set", map[string]interface{}{
		"line_item_id": lineItem.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixSubscriptionLineItem, tenantID, environmentID, lineItem.ID)
	r.cache.Set(ctx, cacheKey, lineItem, cache.ExpiryDefaultInMemory)
}

func (r *subscriptionLineItemRepository) GetCache(ctx context.Context, key string) *subscription.SubscriptionLineItem {
	span := cache.StartCacheSpan(ctx, "subscription_line_item", "get", map[string]interface{}{
		"line_item_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixSubscriptionLineItem, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*subscription.SubscriptionLineItem)
	}
	return nil
}

func (r *subscriptionLineItemRepository) DeleteCache(ctx context.Context, lineItemID string) {
	span := cache.StartCacheSpan(ctx, "subscription_line_item", "delete", map[string]interface{}{
		"line_item_id": lineItemID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixSubscriptionLineItem, tenantID, environmentID, lineItemID)
	r.cache.Delete(ctx, cacheKey)
}
