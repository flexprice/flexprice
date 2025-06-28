package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCreditGrantStore provides an in-memory implementation of creditgrant.Repository for testing
type InMemoryCreditGrantStore struct {
	*InMemoryStore[*creditgrant.CreditGrant]
}

// NewInMemoryCreditGrantStore creates a new in-memory credit grant store
func NewInMemoryCreditGrantStore() *InMemoryCreditGrantStore {
	return &InMemoryCreditGrantStore{
		InMemoryStore: NewInMemoryStore[*creditgrant.CreditGrant](),
	}
}

// Helper to copy credit grant
func copyCreditGrant(cg *creditgrant.CreditGrant) *creditgrant.CreditGrant {
	if cg == nil {
		return nil
	}

	copy := *cg

	// Deep copy pointers
	if cg.PlanID != nil {
		planID := *cg.PlanID
		copy.PlanID = &planID
	}
	if cg.SubscriptionID != nil {
		subscriptionID := *cg.SubscriptionID
		copy.SubscriptionID = &subscriptionID
	}
	if cg.Period != nil {
		period := *cg.Period
		copy.Period = &period
	}
	if cg.PeriodCount != nil {
		periodCount := *cg.PeriodCount
		copy.PeriodCount = &periodCount
	}
	if cg.ExpirationDuration != nil {
		expirationDuration := *cg.ExpirationDuration
		copy.ExpirationDuration = &expirationDuration
	}
	if cg.ExpirationDurationUnit != nil {
		expirationDurationUnit := *cg.ExpirationDurationUnit
		copy.ExpirationDurationUnit = &expirationDurationUnit
	}
	if cg.Priority != nil {
		priority := *cg.Priority
		copy.Priority = &priority
	}

	// Deep copy metadata
	if cg.Metadata != nil {
		copy.Metadata = lo.Assign(map[string]string{}, cg.Metadata)
	}

	return &copy
}

// Create creates a new credit grant
func (s *InMemoryCreditGrantStore) Create(ctx context.Context, cg *creditgrant.CreditGrant) (*creditgrant.CreditGrant, error) {
	if err := cg.Validate(); err != nil {
		return nil, err
	}

	// Set environment ID from context if not already set
	if cg.EnvironmentID == "" {
		cg.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	err := s.InMemoryStore.Create(ctx, cg.ID, copyCreditGrant(cg))
	if err != nil {
		return nil, err
	}

	return copyCreditGrant(cg), nil
}

// CreateBulk creates multiple credit grants
func (s *InMemoryCreditGrantStore) CreateBulk(ctx context.Context, creditGrants []*creditgrant.CreditGrant) ([]*creditgrant.CreditGrant, error) {
	var result []*creditgrant.CreditGrant

	for _, cg := range creditGrants {
		if err := cg.Validate(); err != nil {
			return nil, err
		}

		// Set environment ID from context if not already set
		if cg.EnvironmentID == "" {
			cg.EnvironmentID = types.GetEnvironmentID(ctx)
		}

		err := s.InMemoryStore.Create(ctx, cg.ID, copyCreditGrant(cg))
		if err != nil {
			return nil, err
		}

		result = append(result, copyCreditGrant(cg))
	}

	return result, nil
}

// Get retrieves a credit grant by ID
func (s *InMemoryCreditGrantStore) Get(ctx context.Context, id string) (*creditgrant.CreditGrant, error) {
	cg, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Check if archived (soft deleted)
	if cg.Status == types.StatusArchived {
		return nil, ierr.NewError("credit grant not found").
			WithHint("No credit grant found with the given ID").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrNotFound)
	}

	return copyCreditGrant(cg), nil
}

// List retrieves credit grants based on filter
func (s *InMemoryCreditGrantStore) List(ctx context.Context, filter *types.CreditGrantFilter) ([]*creditgrant.CreditGrant, error) {
	items, err := s.InMemoryStore.List(ctx, filter, creditGrantFilterFn, creditGrantSortFn)
	if err != nil {
		return nil, err
	}

	return lo.Map(items, func(cg *creditgrant.CreditGrant, _ int) *creditgrant.CreditGrant {
		return copyCreditGrant(cg)
	}), nil
}

// ListAll retrieves all credit grants based on filter without pagination
func (s *InMemoryCreditGrantStore) ListAll(ctx context.Context, filter *types.CreditGrantFilter) ([]*creditgrant.CreditGrant, error) {
	if filter == nil {
		filter = types.NewNoLimitCreditGrantFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}

	return s.List(ctx, filter)
}

// Count counts credit grants based on filter
func (s *InMemoryCreditGrantStore) Count(ctx context.Context, filter *types.CreditGrantFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, creditGrantFilterFn)
}

// Update updates an existing credit grant
func (s *InMemoryCreditGrantStore) Update(ctx context.Context, cg *creditgrant.CreditGrant) (*creditgrant.CreditGrant, error) {
	if err := cg.Validate(); err != nil {
		return nil, err
	}

	err := s.InMemoryStore.Update(ctx, cg.ID, copyCreditGrant(cg))
	if err != nil {
		return nil, err
	}

	return copyCreditGrant(cg), nil
}

// Delete deletes a credit grant by ID (soft delete by setting status to archived)
func (s *InMemoryCreditGrantStore) Delete(ctx context.Context, id string) error {
	cg, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return err
	}

	// Soft delete by setting status to archived
	cg.Status = types.StatusArchived
	return s.InMemoryStore.Update(ctx, id, cg)
}

// DeleteBulk deletes multiple credit grants by IDs (soft delete by setting status to archived)
func (s *InMemoryCreditGrantStore) DeleteBulk(ctx context.Context, ids []string) error {
	for _, id := range ids {
		if err := s.Delete(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// GetByPlan retrieves credit grants for a specific plan
func (s *InMemoryCreditGrantStore) GetByPlan(ctx context.Context, planID string) ([]*creditgrant.CreditGrant, error) {
	filter := &types.CreditGrantFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
		PlanIDs:     []string{planID},
	}
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)

	return s.List(ctx, filter)
}

// GetBySubscription retrieves credit grants for a specific subscription
func (s *InMemoryCreditGrantStore) GetBySubscription(ctx context.Context, subscriptionID string) ([]*creditgrant.CreditGrant, error) {
	filter := &types.CreditGrantFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		SubscriptionIDs: []string{subscriptionID},
	}
	filter.QueryFilter.Status = lo.ToPtr(types.StatusPublished)

	return s.List(ctx, filter)
}

// creditGrantFilterFn implements filtering logic for credit grants
func creditGrantFilterFn(ctx context.Context, cg *creditgrant.CreditGrant, filter interface{}) bool {
	f, ok := filter.(*types.CreditGrantFilter)
	if !ok {
		return false
	}

	// Apply tenant filter
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" && cg.TenantID != tenantID {
		return false
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, cg.EnvironmentID) {
		return false
	}

	// Check status filter
	if f.QueryFilter != nil && f.QueryFilter.Status != nil {
		if cg.Status != *f.QueryFilter.Status {
			return false
		}
	}

	// Check plan IDs filter
	if len(f.PlanIDs) > 0 {
		if cg.PlanID == nil || !lo.Contains(f.PlanIDs, *cg.PlanID) {
			return false
		}
	}

	// Check subscription IDs filter
	if len(f.SubscriptionIDs) > 0 {
		if cg.SubscriptionID == nil || !lo.Contains(f.SubscriptionIDs, *cg.SubscriptionID) {
			return false
		}
	}

	return true
}

// creditGrantSortFn implements sorting logic for credit grants
func creditGrantSortFn(i, j *creditgrant.CreditGrant) bool {
	// Default sort by created_at desc
	return i.CreatedAt.After(j.CreatedAt)
}

// Clear removes all credit grants from the store
func (s *InMemoryCreditGrantStore) Clear() {
	s.InMemoryStore.Clear()
}
