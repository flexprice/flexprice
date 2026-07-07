package testutil

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InMemorySubscriptionStore implements subscription.Repository
type InMemorySubscriptionStore struct {
	*InMemoryStore[*subscription.Subscription]
	lineItems     map[string][]*subscription.SubscriptionLineItem // map[subscriptionID][]lineItems (initial batch from CreateWithLineItems)
	lineItemStore *InMemorySubscriptionLineItemStore              // optional: when set, GetWithLineItems merges in line items created via SubscriptionLineItemRepo.Create
	pauses        map[string][]*subscription.SubscriptionPause    // map[subscriptionID][]pauses
	pauseByID     map[string]*subscription.SubscriptionPause      // map[pauseID]pause
}

func NewInMemorySubscriptionStore() *InMemorySubscriptionStore {
	return &InMemorySubscriptionStore{
		InMemoryStore: NewInMemoryStore[*subscription.Subscription](),
		lineItems:     make(map[string][]*subscription.SubscriptionLineItem),
		pauses:        make(map[string][]*subscription.SubscriptionPause),
		pauseByID:     make(map[string]*subscription.SubscriptionPause),
	}
}

// subscriptionFilterFn implements filtering logic for subscriptions
func subscriptionFilterFn(ctx context.Context, sub *subscription.Subscription, filter interface{}) bool {
	if sub == nil {
		return false
	}

	f, ok := filter.(*types.SubscriptionFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if sub.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, sub.EnvironmentID) {
		return false
	}

	// Filter by subscription IDs
	if len(f.SubscriptionIDs) > 0 && !lo.Contains(f.SubscriptionIDs, sub.ID) {
		return false
	}

	// Filter by customer ID
	if f.CustomerID != "" && sub.CustomerID != f.CustomerID {
		return false
	}

	// Filter by customer IDs
	if len(f.CustomerIDs) > 0 && !lo.Contains(f.CustomerIDs, sub.CustomerID) {
		return false
	}

	// Filter by invoicing customer IDs
	if len(f.InvoicingCustomerIDs) > 0 {
		if sub.InvoicingCustomerID == nil || !lo.Contains(f.InvoicingCustomerIDs, *sub.InvoicingCustomerID) {
			return false
		}
	}

	// Filter by plan ID
	if f.PlanID != "" && sub.PlanID != f.PlanID {
		return false
	}

	// Filter by parent subscription IDs
	if len(f.ParentSubscriptionIDs) > 0 {
		if sub.ParentSubscriptionID == nil || !lo.Contains(f.ParentSubscriptionIDs, *sub.ParentSubscriptionID) {
			return false
		}
	}

	// Filter by subscription type
	if len(f.SubscriptionTypes) > 0 && !lo.Contains(f.SubscriptionTypes, sub.SubscriptionType) {
		return false
	}

	// Filter by subscription status
	if len(f.SubscriptionStatus) > 0 && !lo.Contains(f.SubscriptionStatus, sub.SubscriptionStatus) {
		return false
	}

	// Default to active when the client did not constrain subscription_status
	// (neither top-level nor DSL filters) — mirrors the real Ent repo
	// (internal/repository/ent/subscription.go applyEntityQueryOptions).
	if f.SubscriptionStatus == nil && !subscriptionFiltersConstrainStatus(f.Filters) {
		if sub.SubscriptionStatus != types.SubscriptionStatusActive {
			return false
		}
	}

	// Filter by billing cadence
	if len(f.BillingCadence) > 0 && !lo.Contains(f.BillingCadence, sub.BillingCadence) {
		return false
	}

	// Filter by billing period
	if len(f.BillingPeriod) > 0 && !lo.Contains(f.BillingPeriod, sub.BillingPeriod) {
		return false
	}

	// Filter by subscription status not in
	if len(f.SubscriptionStatusNotIn) > 0 && lo.Contains(f.SubscriptionStatusNotIn, sub.SubscriptionStatus) {
		return false
	}

	if f.EffectiveDateForUpdate != nil {
		d := *f.EffectiveDateForUpdate
		periodEnded := !sub.CurrentPeriodEnd.After(d)
		cancelEffective := sub.CancelAt != nil && !sub.CancelAt.After(d)
		if !periodEnded && !cancelEffective {
			return false
		}
	}

	if f.TrialEndDueLTE != nil {
		d := *f.TrialEndDueLTE
		if sub.TrialEnd == nil || sub.TrialEnd.After(d) {
			return false
		}
	}

	// Time range filter — mirrors the real Ent repo: current_period_start >= StartTime,
	// current_period_end <= EndTime (NOT created_at).
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && sub.CurrentPeriodStart.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && sub.CurrentPeriodEnd.After(*f.EndTime) {
			return false
		}
	}

	// Filter by active at — mirrors the real Ent repo:
	// start_date <= ActiveAt AND (end_date > ActiveAt OR end_date IS NULL).
	if f.ActiveAt != nil {
		if sub.StartDate.After(*f.ActiveAt) {
			return false
		}
		if sub.EndDate != nil && !sub.EndDate.After(*f.ActiveAt) {
			return false
		}
	}

	return true
}

// subscriptionFiltersConstrainStatus reports whether any DSL filter condition targets
// subscription_status, mirroring SubscriptionQueryOptions.filtersConstrainSubscriptionStatus
// in internal/repository/ent/subscription.go.
func subscriptionFiltersConstrainStatus(filters []*types.FilterCondition) bool {
	for _, fc := range filters {
		if fc == nil || fc.Field == nil {
			continue
		}
		if *fc.Field == "subscription_status" {
			return true
		}
	}
	return false
}

// subscriptionSortFn implements sorting logic for subscriptions
func subscriptionSortFn(i, j *subscription.Subscription) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemorySubscriptionStore) Create(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil {
		return ierr.NewError("subscription cannot be nil").
			WithHint("Subscription data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if sub.EnvironmentID == "" {
		sub.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, sub.ID, sub)
	if err != nil {
		if ierr.IsAlreadyExists(err) {
			return ierr.WithError(err).
				WithHint("A subscription with this ID already exists").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemorySubscriptionStore) Get(ctx context.Context, id string) (*subscription.Subscription, error) {
	sub, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Subscription not found").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	// Attach line items if they exist
	if items, ok := s.lineItems[id]; ok {
		sub.LineItems = items
	}
	return sub, nil
}

func (s *InMemorySubscriptionStore) List(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	subs, err := s.InMemoryStore.List(ctx, filter, subscriptionFilterFn, subscriptionSortFn)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list subscriptions").
			Mark(ierr.ErrDatabase)
	}
	// Attach line items to each subscription. Mirror GetWithLineItems:
	// when lineItemStore is wired (the standard test setup wires it via
	// SetLineItemStore), source from it so line items added via the
	// SubscriptionLineItemRepo are visible here too. Falls back to the
	// initial-batch map populated by CreateWithLineItems.
	for _, sub := range subs {
		if s.lineItemStore != nil {
			liFilter := types.NewNoLimitSubscriptionLineItemFilter()
			liFilter.SubscriptionIDs = []string{sub.ID}
			liFilter.ActiveFilter = true
			liFilter.CurrentPeriodStart = &sub.CurrentPeriodStart
			items, lerr := s.lineItemStore.List(ctx, liFilter)
			if lerr != nil {
				return nil, lerr
			}
			sub.LineItems = items
			continue
		}
		if items, ok := s.lineItems[sub.ID]; ok {
			sub.LineItems = items
		}
	}
	return subs, nil
}

func (s *InMemorySubscriptionStore) ListByCustomerID(ctx context.Context, customerID string) ([]*subscription.Subscription, error) {
	// Create a filter with customer ID
	filter := &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		CustomerID:  customerID,
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
			types.SubscriptionStatusTrialing,
		},
	}

	// Use the existing List method
	return s.List(ctx, filter)
}

func (s *InMemorySubscriptionStore) ListByIDs(ctx context.Context, ids []string) ([]*subscription.Subscription, error) {
	return s.ListAll(ctx, &types.SubscriptionFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: ids,
	})
}
func (s *InMemorySubscriptionStore) Count(ctx context.Context, filter *types.SubscriptionFilter) (int, error) {
	count, err := s.InMemoryStore.Count(ctx, filter, subscriptionFilterFn)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count subscriptions").
			Mark(ierr.ErrDatabase)
	}
	return count, nil
}

func (s *InMemorySubscriptionStore) Update(ctx context.Context, sub *subscription.Subscription) error {
	if sub == nil {
		return ierr.NewError("subscription cannot be nil").
			WithHint("Subscription data is required").
			Mark(ierr.ErrValidation)
	}
	err := s.InMemoryStore.Update(ctx, sub.ID, sub)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription not found").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": sub.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": sub.ID,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

func (s *InMemorySubscriptionStore) Delete(ctx context.Context, id string) error {
	// Delete line items first
	delete(s.lineItems, id)
	err := s.InMemoryStore.Delete(ctx, id)
	if err != nil {
		if ierr.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Subscription not found").
				WithReportableDetails(map[string]interface{}{
					"subscription_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete subscription").
			WithReportableDetails(map[string]interface{}{
				"subscription_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}
	return nil
}

// ListAll returns all subscriptions without pagination.
// Mirrors the real Ent repo: reuse the caller's filter as-is, overriding only pagination.
func (s *InMemorySubscriptionStore) ListAll(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	if filter == nil {
		filter = &types.SubscriptionFilter{}
	}
	unlimitedFilter := *filter
	unlimitedFilter.QueryFilter = types.NewNoLimitQueryFilter()

	return s.List(ctx, &unlimitedFilter)
}

// GetSubscriptionsForBillingPeriodUpdate returns subscriptions across all tenants for billing-period jobs.
func (s *InMemorySubscriptionStore) GetSubscriptionsForBillingPeriodUpdate(ctx context.Context, filter *types.SubscriptionFilter) ([]*subscription.Subscription, error) {
	return s.ListAll(ctx, filter)
}

// CreateWithLineItems creates a subscription with its line items
func (s *InMemorySubscriptionStore) CreateWithLineItems(ctx context.Context, sub *subscription.Subscription, items []*subscription.SubscriptionLineItem) error {
	if err := s.Create(ctx, sub); err != nil {
		return err
	}
	s.lineItems[sub.ID] = items
	sub.LineItems = items
	// Mirror DB behavior: line items must be visible to SubscriptionLineItemRepo (e.g. billing reload).
	if s.lineItemStore != nil {
		for _, item := range items {
			if item == nil {
				continue
			}
			if item.SubscriptionID == "" {
				item.SubscriptionID = sub.ID
			}
			if item.ID == "" {
				item.ID = types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM)
			}
			if err := s.lineItemStore.Create(ctx, item); err != nil {
				return err
			}
		}
	}
	return nil
}

// SetLineItemStore sets the line item store so GetWithLineItems can return line items added via SubscriptionLineItemRepo.Create (e.g. AddSubscriptionLineItem).
func (s *InMemorySubscriptionStore) SetLineItemStore(store *InMemorySubscriptionLineItemStore) {
	s.lineItemStore = store
}

// GetWithLineItems gets a subscription with its line items.
// When lineItemStore is set, line items come from SubscriptionLineItemRepo only (mirrors DB + supports Update).
// Otherwise returns the batch from CreateWithLineItems.
func (s *InMemorySubscriptionStore) GetWithLineItems(ctx context.Context, id string) (*subscription.Subscription, []*subscription.SubscriptionLineItem, error) {
	sub, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	if s.lineItemStore != nil {
		filter := types.NewNoLimitSubscriptionLineItemFilter()
		filter.SubscriptionIDs = []string{id}
		filter.ActiveFilter = true
		filter.CurrentPeriodStart = &sub.CurrentPeriodStart
		items, lerr := s.lineItemStore.List(ctx, filter)
		if lerr != nil {
			return nil, nil, lerr
		}
		sub.LineItems = items
		return sub, items, nil
	}
	items := s.lineItems[id]
	if items == nil {
		items = []*subscription.SubscriptionLineItem{}
	}
	sub.LineItems = items
	return sub, items, nil
}

// CreatePause creates a new subscription pause
func (s *InMemorySubscriptionStore) CreatePause(ctx context.Context, pause *subscription.SubscriptionPause) error {
	if pause == nil {
		return ierr.NewError("pause cannot be nil").
			WithHint("Pause data is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if pause.EnvironmentID == "" {
		pause.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	// Store the pause
	s.pauseByID[pause.ID] = pause

	// Add to subscription's pauses
	s.pauses[pause.SubscriptionID] = append(s.pauses[pause.SubscriptionID], pause)

	return nil
}

// GetPause gets a subscription pause by ID
func (s *InMemorySubscriptionStore) GetPause(ctx context.Context, id string) (*subscription.SubscriptionPause, error) {
	pause, ok := s.pauseByID[id]
	if !ok {
		return nil, ierr.NewError("pause not found").
			WithHint("Pause not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return pause, nil
}

// UpdatePause updates a subscription pause
func (s *InMemorySubscriptionStore) UpdatePause(ctx context.Context, pause *subscription.SubscriptionPause) error {
	if pause == nil {
		return ierr.NewError("pause cannot be nil").
			WithHint("Pause data is required").
			Mark(ierr.ErrValidation)
	}

	// Check if pause exists
	_, ok := s.pauseByID[pause.ID]
	if !ok {
		return ierr.NewError("pause not found").
			WithHint("Pause not found").
			WithReportableDetails(map[string]interface{}{
				"id": pause.ID,
			}).
			Mark(ierr.ErrNotFound)
	}

	// Update the pause
	s.pauseByID[pause.ID] = pause

	// Update in subscription's pauses
	for i, p := range s.pauses[pause.SubscriptionID] {
		if p.ID == pause.ID {
			s.pauses[pause.SubscriptionID][i] = pause
			break
		}
	}

	return nil
}

// ListPauses lists all pauses for a subscription
func (s *InMemorySubscriptionStore) ListPauses(ctx context.Context, subscriptionID string) ([]*subscription.SubscriptionPause, error) {
	pauses := s.pauses[subscriptionID]
	return pauses, nil
}

// GetWithPauses gets a subscription with its pauses
func (s *InMemorySubscriptionStore) GetWithPauses(ctx context.Context, id string) (*subscription.Subscription, []*subscription.SubscriptionPause, error) {
	sub, err := s.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	pauses := s.pauses[id]
	sub.Pauses = pauses
	return sub, pauses, nil
}

// ListSubscriptionsDueForRenewal retrieves all active subscriptions that are due for renewal in 24 hours
func (s *InMemorySubscriptionStore) ListSubscriptionsDueForRenewal(ctx context.Context, referenceTime time.Time) ([]*subscription.Subscription, error) {
	referenceTime = referenceTime.UTC()
	targetTime := referenceTime.Add(24 * time.Hour)
	windowStart := targetTime.Add(-15 * time.Minute)

	filter := &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
		},
	}

	allSubs, err := s.ListAll(ctx, filter)
	if err != nil {
		return nil, err
	}

	return lo.Filter(allSubs, func(sub *subscription.Subscription, _ int) bool {
		return !sub.CurrentPeriodEnd.Before(windowStart) && sub.CurrentPeriodEnd.Before(targetTime) && !sub.CancelAtPeriodEnd
	}), nil
}

// GetRecentSubscriptionsByPlan returns subscription counts grouped by plan for last 7 days
func (s *InMemorySubscriptionStore) GetRecentSubscriptionsByPlan(ctx context.Context) ([]types.SubscriptionPlanCount, error) {
	now := time.Now().UTC()
	sevenDaysAgo := now.AddDate(0, 0, -7)

	// Mirror the real repo's raw SQL: active + published subscriptions created in the last 7 days.
	allActive, err := s.ListAll(ctx, &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
		},
	})
	if err != nil {
		return nil, err
	}
	subscriptions := lo.Filter(allActive, func(sub *subscription.Subscription, _ int) bool {
		return sub.Status == types.StatusPublished && !sub.CreatedAt.Before(sevenDaysAgo)
	})

	// Group by plan
	planCounts := make(map[string]*types.SubscriptionPlanCount)
	for _, sub := range subscriptions {
		if sub.PlanID == "" {
			continue
		}

		if pc, exists := planCounts[sub.PlanID]; exists {
			pc.Count++
		} else {
			planCounts[sub.PlanID] = &types.SubscriptionPlanCount{
				PlanID:   sub.PlanID,
				PlanName: sub.PlanID, // Use PlanID as PlanName since we don't have plan data in test store
				Count:    1,
			}
		}
	}

	// Convert map to slice
	result := make([]types.SubscriptionPlanCount, 0, len(planCounts))
	for _, pc := range planCounts {
		result = append(result, *pc)
	}

	return result, nil
}

// GetSubscriptionsWithAutoInvoiceThreshold returns active subscriptions where
// auto_invoice_threshold is set on the subscription (see subscription.Repository).
func (s *InMemorySubscriptionStore) GetSubscriptionsWithAutoInvoiceThreshold(ctx context.Context, limit, offset int) ([]*subscription.Subscription, error) {
	all, err := s.ListAll(ctx, &types.SubscriptionFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		SubscriptionStatus: []types.SubscriptionStatus{
			types.SubscriptionStatusActive,
		},
	})
	if err != nil {
		return nil, err
	}

	var results []*subscription.Subscription
	for _, sub := range all {
		if sub.SubscriptionType != types.SubscriptionTypeStandalone || sub.Status != types.StatusPublished {
			continue
		}
		if sub.AutoInvoiceThreshold != nil && sub.AutoInvoiceThreshold.GreaterThan(decimal.Zero) {
			results = append(results, sub)
		}
	}

	// Apply offset and limit
	if offset >= len(results) {
		return []*subscription.Subscription{}, nil
	}
	results = results[offset:]
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// Clear removes all data from the store
func (s *InMemorySubscriptionStore) Clear() {
	// Clear the base subscription store
	s.InMemoryStore.Clear()

	// Clear line items
	s.lineItems = make(map[string][]*subscription.SubscriptionLineItem)

	// Clear pauses
	s.pauses = make(map[string][]*subscription.SubscriptionPause)
	s.pauseByID = make(map[string]*subscription.SubscriptionPause)
}
