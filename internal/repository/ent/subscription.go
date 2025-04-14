package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/subscription"
	"github.com/flexprice/flexprice/ent/subscriptionpause"
	domainSub "github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

	// Set environment ID from context if not already set
	if sub.EnvironmentID == "" {
		sub.EnvironmentID = types.GetEnvironmentID(ctx)
	}

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
		SetBillingCadence(string(sub.BillingCadence)).
		SetBillingPeriod(string(sub.BillingPeriod)).
		SetBillingPeriodCount(sub.BillingPeriodCount).
		SetStatus(string(sub.Status)).
		SetCreatedBy(sub.CreatedBy).
		SetUpdatedBy(sub.UpdatedBy).
		SetEnvironmentID(sub.EnvironmentID).
		SetVersion(1).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create subscription: %w", err)
	}

	// Update the input subscription with created data
	*sub = *domainSub.GetSubscriptionFromEnt(subscription)
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
			return nil, ierr.NewError("subscription not found").
				WithHint("Subscription not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription").
			Mark(ierr.ErrDatabase)
	}

	return domainSub.GetSubscriptionFromEnt(sub), nil
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
		SetPauseStatus(string(sub.PauseStatus)).
		SetCancelAtPeriodEnd(sub.CancelAtPeriodEnd).
		SetUpdatedAt(now).
		SetUpdatedBy(types.GetUserID(ctx)).
		AddVersion(1) // Increment version atomically

	if sub.ActivePauseID != nil {
		query.SetActivePauseID(*sub.ActivePauseID)
	} else {
		query.ClearActivePauseID()
	}

	// Execute update
	n, err := query.Save(ctx)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update subscription").
			Mark(ierr.ErrDatabase)
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
			return ierr.WithError(err).
				WithHint("Failed to check if subscription exists").
				Mark(ierr.ErrDatabase)
		}
		if !exists {
			return ierr.NewError("subscription not found").
				WithHint("Subscription not found").
				Mark(ierr.ErrNotFound)
		}
		// Record exists but version mismatch
		return ierr.NewError("version conflict").
			WithHint("Version conflict").
			WithReportableDetails(
				map[string]any{
					"subscription_id":  sub.ID,
					"expected_version": sub.Version,
					"actual_version":   sub.Version + 1,
				},
			).
			Mark(ierr.ErrVersionConflict)
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
			return ierr.NewError("subscription not found").
				WithHint("Subscription not found").
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete subscription").
			Mark(ierr.ErrDatabase)
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
	if filter.WithLineItems {
		query = query.WithLineItems()
	}

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
		result[i] = domainSub.GetSubscriptionFromEnt(sub)
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
		result[i] = domainSub.GetSubscriptionFromEnt(sub)
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

func (o SubscriptionQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query SubscriptionQuery) SubscriptionQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(subscription.EnvironmentIDEQ(environmentID))
	}
	return query
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
func (o *SubscriptionQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.SubscriptionFilter, query SubscriptionQuery) SubscriptionQuery {
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

	// Apply subscription status not in filter
	if len(f.SubscriptionStatusNotIn) > 0 {
		statuses := make([]string, len(f.SubscriptionStatusNotIn))
		for i, status := range f.SubscriptionStatusNotIn {
			statuses[i] = string(status)
		}
		query = query.Where(subscription.SubscriptionStatusNotIn(statuses...))
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

// Add new methods for line items
func (r *subscriptionRepository) CreateWithLineItems(ctx context.Context, sub *domainSub.Subscription, items []*domainSub.SubscriptionLineItem) error {
	return r.client.WithTx(ctx, func(ctx context.Context) error {
		// Create subscription first
		if err := r.Create(ctx, sub); err != nil {
			return fmt.Errorf("failed to create subscription: %w", err)
		}

		// Create line items
		client := r.client.Querier(ctx)
		bulk := make([]*ent.SubscriptionLineItemCreate, len(items))
		for i, item := range items {
			// Set environment ID from context if not already set
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
				SetCreatedAt(time.Now()).
				SetUpdatedAt(time.Now())
		}

		if err := client.SubscriptionLineItem.CreateBulk(bulk...).Exec(ctx); err != nil {
			return fmt.Errorf("failed to create subscription line items: %w", err)
		}

		return nil
	})
}

func (r *subscriptionRepository) GetWithLineItems(ctx context.Context, id string) (*domainSub.Subscription, []*domainSub.SubscriptionLineItem, error) {
	client := r.client.Querier(ctx)
	sub, err := client.Subscription.Query().
		Where(
			subscription.ID(id),
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.Status(string(types.StatusPublished)),
		).
		WithLineItems().
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, ierr.NewError("subscription not found").
				WithHint("Subscription not found").
				Mark(ierr.ErrNotFound)
		}
		return nil, nil, ierr.WithError(err).
			WithHint("Failed to get subscription with line items").
			Mark(ierr.ErrDatabase)
	}

	s := domainSub.GetSubscriptionFromEnt(sub)
	s.LineItems = domainSub.GetLineItemFromEntList(sub.Edges.LineItems)

	return s, s.LineItems, nil
}

// CreatePause creates a new subscription pause
func (r *subscriptionRepository) CreatePause(ctx context.Context, pause *domainSub.SubscriptionPause) error {
	client := r.client.Querier(ctx)

	// Set environment ID from context if not already set
	if pause.EnvironmentID == "" {
		pause.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	p, err := client.SubscriptionPause.Create().
		SetID(pause.ID).
		SetTenantID(pause.TenantID).
		SetSubscriptionID(pause.SubscriptionID).
		SetPauseStatus(string(pause.PauseStatus)).
		SetPauseMode(string(pause.PauseMode)).
		SetResumeMode(string(pause.ResumeMode)).
		SetPauseStart(pause.PauseStart).
		SetNillablePauseEnd(pause.PauseEnd).
		SetNillableResumedAt(pause.ResumedAt).
		SetOriginalPeriodStart(pause.OriginalPeriodStart).
		SetOriginalPeriodEnd(pause.OriginalPeriodEnd).
		SetReason(pause.Reason).
		SetMetadata(pause.Metadata).
		SetStatus(string(pause.Status)).
		SetCreatedBy(pause.CreatedBy).
		SetUpdatedBy(pause.UpdatedBy).
		SetEnvironmentID(pause.EnvironmentID).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create subscription pause").
			Mark(ierr.ErrDatabase)
	}

	// Update the input pause with created data
	*pause = *domainSub.SubscriptionPauseFromEnt(p)
	return nil
}

// GetPause gets a subscription pause by ID
func (r *subscriptionRepository) GetPause(ctx context.Context, id string) (*domainSub.SubscriptionPause, error) {
	client := r.client.Querier(ctx)
	p, err := client.SubscriptionPause.Query().
		Where(
			subscriptionpause.ID(id),
			subscriptionpause.TenantID(types.GetTenantID(ctx)),
			subscriptionpause.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Subscription pause %s not found", id).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get subscription pause").
			Mark(ierr.ErrDatabase)
	}

	return domainSub.SubscriptionPauseFromEnt(p), nil
}

// UpdatePause updates a subscription pause
func (r *subscriptionRepository) UpdatePause(ctx context.Context, pause *domainSub.SubscriptionPause) error {
	client := r.client.Querier(ctx)
	now := time.Now().UTC()

	p, err := client.SubscriptionPause.Query().
		Where(
			subscriptionpause.ID(pause.ID),
			subscriptionpause.TenantID(types.GetTenantID(ctx)),
			subscriptionpause.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Subscription pause %s not found", pause.ID).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to get subscription pause for update").
			Mark(ierr.ErrDatabase)
	}

	_, err = p.Update().
		SetPauseStatus(string(pause.PauseStatus)).
		SetResumeMode(string(pause.ResumeMode)).
		SetNillablePauseEnd(pause.PauseEnd).
		SetNillableResumedAt(pause.ResumedAt).
		SetReason(pause.Reason).
		SetMetadata(pause.Metadata).
		SetUpdatedBy(pause.UpdatedBy).
		SetUpdatedAt(now).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to update subscription pause").
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// ListPauses lists all pauses for a subscription
func (r *subscriptionRepository) ListPauses(ctx context.Context, subscriptionID string) ([]*domainSub.SubscriptionPause, error) {
	client := r.client.Querier(ctx)
	pauses, err := client.SubscriptionPause.Query().
		Where(
			subscriptionpause.SubscriptionID(subscriptionID),
			subscriptionpause.TenantID(types.GetTenantID(ctx)),
			subscriptionpause.EnvironmentID(types.GetEnvironmentID(ctx)),
			subscriptionpause.Status(string(types.StatusPublished)),
		).
		Order(ent.Desc(subscriptionpause.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscription pauses").
			Mark(ierr.ErrDatabase)
	}

	return domainSub.SubscriptionPauseListFromEnt(pauses), nil
}

// GetWithPauses gets a subscription with its pauses
func (r *subscriptionRepository) GetWithPauses(ctx context.Context, id string) (*domainSub.Subscription, []*domainSub.SubscriptionPause, error) {
	client := r.client.Querier(ctx)
	sub, err := client.Subscription.Query().
		Where(
			subscription.ID(id),
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.EnvironmentID(types.GetEnvironmentID(ctx)),
			subscription.Status(string(types.StatusPublished)),
		).
		WithPauses(func(q *ent.SubscriptionPauseQuery) {
			q.Where(subscriptionpause.Status(string(types.StatusPublished)))
			q.Order(ent.Desc(subscriptionpause.FieldCreatedAt))
		}).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil, ierr.WithError(err).
				WithHintf("Subscription %s not found", id).
				Mark(ierr.ErrNotFound)
		}
		return nil, nil, ierr.WithError(err).
			WithHint("Failed to get subscription with pauses").
			Mark(ierr.ErrDatabase)
	}

	subscription := domainSub.GetSubscriptionFromEnt(sub)
	var pauses []*domainSub.SubscriptionPause
	if sub.Edges.Pauses != nil {
		pauses = domainSub.SubscriptionPauseListFromEnt(sub.Edges.Pauses)
	}

	return subscription, pauses, nil
}

// ListByCustomerID retrieves all active subscriptions for a customer and includes line items
func (r *subscriptionRepository) ListByCustomerID(ctx context.Context, customerID string) ([]*domainSub.Subscription, error) {
	r.logger.Debugw("listing subscriptions by customer ID",
		"customer_id", customerID)

	// Create a filter with customer ID
	filter := &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerID:  customerID,
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
		},
		WithLineItems: true,
	}

	// Use the existing List method
	return r.List(ctx, filter)
}

// ListByIDs retrieves subscriptions by their IDs and includes line items
func (r *subscriptionRepository) ListByIDs(ctx context.Context, subscriptionIDs []string) ([]*domainSub.Subscription, error) {
	if len(subscriptionIDs) == 0 {
		return []*domainSub.Subscription{}, nil
	}

	r.logger.Debugw("listing subscriptions by IDs", "subscription_ids", subscriptionIDs)

	// Since SubscriptionFilter doesn't have a SubscriptionIDs field,
	// we need to use a direct query instead of the List method
	client := r.client.Querier(ctx)
	query := client.Subscription.Query().
		WithLineItems().
		Where(
			subscription.IDIn(subscriptionIDs...),
			subscription.TenantID(types.GetTenantID(ctx)),
			subscription.EnvironmentID(types.GetEnvironmentID(ctx)),
			subscription.Status(string(types.StatusPublished)),
		)

	// Order by created date descending
	query = query.Order(ent.Desc(subscription.FieldCreatedAt))

	subs, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscriptions by IDs").
			WithReportableDetails(map[string]interface{}{
				"subscription_ids": subscriptionIDs,
			}).
			Mark(ierr.ErrDatabase)
	}

	// Convert to domain model
	result := make([]*domainSub.Subscription, len(subs))
	for i, sub := range subs {
		result[i] = domainSub.GetSubscriptionFromEnt(sub)
	}

	return result, nil
}
