package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscription"
	domainSub "github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type subscriptionRepository struct {
	client    postgres.IClient
	logger    *logger.Logger
	queryOpts SubscriptionQueryOptions
}

func NewSubscriptionRepository(client postgres.IClient, logger *logger.Logger) domainSub.Repository {
	return &subscriptionRepository{
		client:    client,
		logger:    logger,
		queryOpts: SubscriptionQueryOptions{},
	}
}

func (r *subscriptionRepository) Create(ctx context.Context, sub *domainSub.Subscription) error {
	client := r.client.Querier(ctx)
	subscription, err := client.Subscription.Create().
		SetID(sub.ID).
		SetTenantID(sub.TenantID).
		SetLookupKey(sub.LookupKey).
		SetCustomerID(sub.CustomerID).
		SetPlanID(sub.PlanID).
		SetSubscriptionStatus(string(sub.SubscriptionStatus)).
		SetCurrency(sub.Currency).
		SetBillingAnchor(sub.BillingAnchor).
		SetStartDate(sub.StartDate).
		SetNillableEndDate(sub.EndDate).
		SetCurrentPeriodStart(sub.CurrentPeriodStart).
		SetCurrentPeriodEnd(sub.CurrentPeriodEnd).
		SetNillableCancelledAt(sub.CancelledAt).
		SetNillableCancelAt(sub.CancelAt).
		SetCancelAtPeriodEnd(sub.CancelAtPeriodEnd).
		SetNillableTrialStart(sub.TrialStart).
		SetNillableTrialEnd(sub.TrialEnd).
		SetInvoiceCadence(string(sub.InvoiceCadence)).
		SetBillingCadence(string(sub.BillingCadence)).
		SetBillingPeriod(string(sub.BillingPeriod)).
		SetBillingPeriodCount(sub.BillingPeriodCount).
		SetStatus(string(sub.Status)).
		SetCreatedBy(sub.CreatedBy).
		SetUpdatedBy(sub.UpdatedBy).
		SetVersion(1).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Update the input subscription with created data
	*sub = *toDomainSubscription(subscription)
	return nil
}

func (r *subscriptionRepository) Get(ctx context.Context, id string) (*domainSub.Subscription, error) {
	client := r.client.Querier(ctx)
	sub, err := client.Subscription.Query().
		Where(
			subscription.ID(id),
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, domainSub.NewNotFoundError(id)
		}
		return nil, errors.WithOp(err, "repository.subscription.Get")
	}

	return toDomainSubscription(sub), nil
}

func (r *subscriptionRepository) Update(ctx context.Context, sub *domainSub.Subscription) error {
	client := r.client.Querier(ctx)
	now := time.Now().UTC()

	// Use predicate-based update for optimistic locking
	query := client.Subscription.Update().
		Where(
			subscription.ID(sub.ID),
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.Status(string(types.StatusPublished)),
			subscription.Version(sub.Version), // Version check for optimistic locking
		)

	// Set all fields
	query.
		SetLookupKey(sub.LookupKey).
		SetSubscriptionStatus(string(sub.SubscriptionStatus)).
		SetCurrentPeriodStart(sub.CurrentPeriodStart).
		SetCurrentPeriodEnd(sub.CurrentPeriodEnd).
		SetNillableCancelledAt(sub.CancelledAt).
		SetNillableCancelAt(sub.CancelAt).
		SetCancelAtPeriodEnd(sub.CancelAtPeriodEnd).
		SetUpdatedAt(now).
		SetUpdatedBy(types.GetUserID(ctx)).
		AddVersion(1) // Increment version atomically

	// Execute update
	n, err := query.Save(ctx)
	if err != nil {
		return errors.WithOp(err, "repository.subscription.Update")
	}
	if n == 0 {
		// No rows were updated - either record doesn't exist or version mismatch
		exists, err := client.Subscription.Query().
			Where(
				subscription.ID(sub.ID),
				subscription.TenantID(types.GetTenantID(ctx)),
			).
			Exist(ctx)
		if err != nil {
			return errors.WithOp(err, "repository.subscription.Update.CheckExists")
		}
		if !exists {
			return domainSub.NewNotFoundError(sub.ID)
		}
		// Record exists but version mismatch
		return domainSub.NewVersionConflictError(sub.ID, sub.Version, sub.Version+1)
	}

	return nil
}

func (r *subscriptionRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)
	err := client.Subscription.UpdateOneID(id).
		Where(
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.Status(string(types.StatusPublished)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Exec(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return domainSub.NewNotFoundError(id)
		}
		return errors.WithOp(err, "repository.subscription.Delete")
	}

	return nil
}

// List retrieves a list of subscriptions based on the provided filter
func (r *subscriptionRepository) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*domainSub.Subscription, error) {
	r.logger.Debugw("listing subscriptions", "filter", filter)

	if filter == nil {
		filter = &types.SubscriptionFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	client := r.client.Querier(ctx)
	query := client.Subscription.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	subs, err := query.All(ctx)
	if err != nil {
		r.logger.Errorw("failed to list subscriptions", "error", err)
		return nil, fmt.Errorf("listing subscriptions: %w", err)
	}

	// Convert to domain model
	result := make([]*domainSub.Subscription, len(subs))
	for i, sub := range subs {
		result[i] = toDomainSubscription(sub)
	}

	return result, nil
}

// ListAll retrieves all subscriptions without pagination
func (r *subscriptionRepository) ListAll(ctx context.Context, filter *types.SubscriptionFilter) ([]*domainSub.Subscription, error) {
	if filter == nil {
		filter = &types.SubscriptionFilter{
			QueryFilter: types.NewNoLimitQueryFilter(),
		}
	} else {
		// Override pagination settings for ListAll
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	return r.List(ctx, filter)
}

// ListAllTenant retrieves all subscriptions across all tenants
// NOTE: This is a potentially expensive operation and to be used only for CRONs
func (r *subscriptionRepository) ListAllTenant(ctx context.Context, filter *types.SubscriptionFilter) ([]*domainSub.Subscription, error) {
	r.logger.Debugw("listing subscriptions for all tenants", "filter", filter)

	if filter == nil {
		filter = &types.SubscriptionFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	if err := filter.Validate(); err != nil {
		return nil, fmt.Errorf("invalid filter: %w", err)
	}

	client := r.client.Querier(ctx)
	query := client.Subscription.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply all query options except tenant filter
	query = ApplySorting(query, filter, r.queryOpts)
	query = ApplyPagination(query, filter, r.queryOpts)
	query = r.queryOpts.ApplyStatusFilter(query, filter.GetStatus())

	subs, err := query.All(ctx)
	if err != nil {
		r.logger.Errorw("failed to list subscriptions", "error", err)
		return nil, fmt.Errorf("listing subscriptions: %w", err)
	}

	// Convert to domain model
	result := make([]*domainSub.Subscription, len(subs))
	for i, sub := range subs {
		result[i] = toDomainSubscription(sub)
	}

	return result, nil
}

// Count returns the total number of subscriptions based on the provided filter
func (r *subscriptionRepository) Count(ctx context.Context, filter *types.SubscriptionFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Subscription.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to count subscriptions: %w", err)
	}
	return count, nil
}

// Query option methods
// SubscriptionQuery type alias for better readability
type SubscriptionQuery = *ent.SubscriptionQuery

// SubscriptionQueryOptions implements BaseQueryOptions for subscription queries
type SubscriptionQueryOptions struct{}

func (o SubscriptionQueryOptions) ApplyTenantFilter(ctx context.Context, query SubscriptionQuery) SubscriptionQuery {
	return query.Where(subscription.TenantID(types.GetTenantID(ctx)))
}

func (o SubscriptionQueryOptions) ApplyStatusFilter(query SubscriptionQuery, status string) SubscriptionQuery {
	if status == "" {
		return query.Where(subscription.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(subscription.Status(status))
}

func (o SubscriptionQueryOptions) ApplySortFilter(query SubscriptionQuery, field string, order string) SubscriptionQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o SubscriptionQueryOptions) ApplyPaginationFilter(query SubscriptionQuery, limit int, offset int) SubscriptionQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o SubscriptionQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return subscription.FieldCreatedAt
	case "updated_at":
		return subscription.FieldUpdatedAt
	case "start_date":
		return subscription.FieldStartDate
	case "end_date":
		return subscription.FieldEndDate
	case "current_period_start":
		return subscription.FieldCurrentPeriodStart
	case "current_period_end":
		return subscription.FieldCurrentPeriodEnd
	default:
		return field
	}
}

// applyEntityQueryOptions applies subscription-specific filters to the query
func (o *SubscriptionQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.SubscriptionFilter, query SubscriptionQuery) SubscriptionQuery {
	if f == nil {
		return query
	}

	// Apply customer filter
	if f.CustomerID != "" {
		query = query.Where(subscription.CustomerID(f.CustomerID))
	}

	// Apply plan filter
	if f.PlanID != "" {
		query = query.Where(subscription.PlanID(f.PlanID))
	}

	// Apply subscription status filter
	if len(f.SubscriptionStatus) > 0 {
		statuses := make([]string, len(f.SubscriptionStatus))
		for i, status := range f.SubscriptionStatus {
			statuses[i] = string(status)
		}
		query = query.Where(subscription.SubscriptionStatusIn(statuses...))
	}

	// Apply invoice cadence filter
	if len(f.InvoiceCadence) > 0 {
		cadences := make([]string, len(f.InvoiceCadence))
		for i, cadence := range f.InvoiceCadence {
			cadences[i] = string(cadence)
		}
		query = query.Where(subscription.InvoiceCadenceIn(cadences...))
	}

	// Apply billing cadence filter
	if len(f.BillingCadence) > 0 {
		cadences := make([]string, len(f.BillingCadence))
		for i, cadence := range f.BillingCadence {
			cadences[i] = string(cadence)
		}
		query = query.Where(subscription.BillingCadenceIn(cadences...))
	}

	// Apply billing period filter
	if len(f.BillingPeriod) > 0 {
		periods := make([]string, len(f.BillingPeriod))
		for i, period := range f.BillingPeriod {
			periods[i] = string(period)
		}
		query = query.Where(subscription.BillingPeriodIn(periods...))
	}

	// Apply canceled filter
	if !f.IncludeCanceled {
		query = query.Where(subscription.CancelledAtIsNil())
	}

	// Apply active at filter
	if f.ActiveAt != nil {
		query = query.Where(
			subscription.And(
				subscription.StartDateLTE(*f.ActiveAt),
				subscription.Or(
					subscription.EndDateGT(*f.ActiveAt),
					subscription.EndDateIsNil(),
				),
			),
		)
	}

	// Apply time range filters
	if f.TimeRangeFilter != nil {
		if f.TimeRangeFilter.StartTime != nil {
			query = query.Where(subscription.CurrentPeriodStartGTE(*f.TimeRangeFilter.StartTime))
		}
		if f.TimeRangeFilter.EndTime != nil {
			query = query.Where(subscription.CurrentPeriodEndLTE(*f.TimeRangeFilter.EndTime))
		}
	}

	return query
}

func toDomainSubscription(sub *ent.Subscription) *domainSub.Subscription {
	return &domainSub.Subscription{
		ID:                 sub.ID,
		LookupKey:          sub.LookupKey,
		CustomerID:         sub.CustomerID,
		PlanID:             sub.PlanID,
		SubscriptionStatus: types.SubscriptionStatus(sub.SubscriptionStatus),
		Currency:           sub.Currency,
		BillingAnchor:      sub.BillingAnchor,
		StartDate:          sub.StartDate,
		EndDate:            sub.EndDate,
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   sub.CurrentPeriodEnd,
		CancelledAt:        sub.CancelledAt,
		CancelAt:           sub.CancelAt,
		CancelAtPeriodEnd:  sub.CancelAtPeriodEnd,
		TrialStart:         sub.TrialStart,
		TrialEnd:           sub.TrialEnd,
		InvoiceCadence:     types.InvoiceCadence(sub.InvoiceCadence),
		BillingCadence:     types.BillingCadence(sub.BillingCadence),
		BillingPeriod:      types.BillingPeriod(sub.BillingPeriod),
		BillingPeriodCount: sub.BillingPeriodCount,
		Version:            sub.Version,
		BaseModel: types.BaseModel{
			TenantID:  sub.TenantID,
			Status:    types.Status(sub.Status),
			CreatedAt: sub.CreatedAt,
			CreatedBy: sub.CreatedBy,
			UpdatedAt: sub.UpdatedAt,
			UpdatedBy: sub.UpdatedBy,
		},
	}
}
