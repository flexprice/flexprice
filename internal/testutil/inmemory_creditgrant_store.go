package testutil

import (
	"context"
	"sort"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/creditgrant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// InMemoryCreditGrantStore provides an in-memory implementation of creditgrant.Repository for testing
type InMemoryCreditGrantStore struct {
	mu           sync.RWMutex
	creditGrants map[string]*creditgrant.CreditGrant
}

// NewInMemoryCreditGrantStore creates a new in-memory credit grant store
func NewInMemoryCreditGrantStore() *InMemoryCreditGrantStore {
	return &InMemoryCreditGrantStore{
		creditGrants: make(map[string]*creditgrant.CreditGrant),
	}
}

// Create creates a new credit grant
func (s *InMemoryCreditGrantStore) Create(ctx context.Context, cg *creditgrant.CreditGrant) (*creditgrant.CreditGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := cg.Validate(); err != nil {
		return nil, err
	}

	// Check if already exists
	if _, exists := s.creditGrants[cg.ID]; exists {
		return nil, ierr.NewError("credit grant already exists").
			WithHint("A credit grant with this ID already exists").
			WithReportableDetails(map[string]interface{}{"id": cg.ID}).
			Mark(ierr.ErrAlreadyExists)
	}

	// Store copy to avoid mutation
	stored := *cg
	s.creditGrants[cg.ID] = &stored

	return &stored, nil
}

// Get retrieves a credit grant by ID
func (s *InMemoryCreditGrantStore) Get(ctx context.Context, id string) (*creditgrant.CreditGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cg, exists := s.creditGrants[id]
	if !exists || cg.Status == types.StatusArchived {
		return nil, ierr.NewError("credit grant not found").
			WithHint("No credit grant found with the given ID").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrNotFound)
	}

	// Return copy to avoid mutation
	result := *cg
	return &result, nil
}

// List retrieves credit grants based on filter
func (s *InMemoryCreditGrantStore) List(ctx context.Context, filter *types.CreditGrantFilter) ([]*creditgrant.CreditGrant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []*creditgrant.CreditGrant

	for _, cg := range s.creditGrants {
		if s.matchesFilter(cg, filter) {
			// Return copy to avoid mutation
			copy := *cg
			results = append(results, &copy)
		}
	}

	// Apply sorting
	if filter != nil && filter.QueryFilter != nil && filter.QueryFilter.Sort != nil {
		sort.Slice(results, func(i, j int) bool {
			switch *filter.QueryFilter.Sort {
			case "created_at":
				if filter.QueryFilter.Order != nil && *filter.QueryFilter.Order == "asc" {
					return results[i].CreatedAt.Before(results[j].CreatedAt)
				}
				return results[i].CreatedAt.After(results[j].CreatedAt)
			case "name":
				if filter.QueryFilter.Order != nil && *filter.QueryFilter.Order == "desc" {
					return results[i].Name > results[j].Name
				}
				return results[i].Name < results[j].Name
			default:
				return results[i].CreatedAt.After(results[j].CreatedAt)
			}
		})
	}

	// Apply pagination
	if filter != nil && filter.QueryFilter != nil {
		offset := filter.GetOffset()
		limit := filter.GetLimit()

		if offset > len(results) {
			return []*creditgrant.CreditGrant{}, nil
		}

		end := offset + limit
		if end > len(results) {
			end = len(results)
		}

		results = results[offset:end]
	}

	return results, nil
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
	s.mu.RLock()
	defer s.mu.RUnlock()

	count := 0
	for _, cg := range s.creditGrants {
		if s.matchesFilter(cg, filter) {
			count++
		}
	}

	return count, nil
}

// Update updates an existing credit grant
func (s *InMemoryCreditGrantStore) Update(ctx context.Context, cg *creditgrant.CreditGrant) (*creditgrant.CreditGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.creditGrants[cg.ID]; !exists {
		return nil, ierr.NewError("credit grant not found").
			WithHint("No credit grant found with the given ID").
			WithReportableDetails(map[string]interface{}{"id": cg.ID}).
			Mark(ierr.ErrNotFound)
	}

	if err := cg.Validate(); err != nil {
		return nil, err
	}

	// Store copy to avoid mutation
	stored := *cg
	s.creditGrants[cg.ID] = &stored

	return &stored, nil
}

// Delete deletes a credit grant by ID (soft delete by setting status to archived)
func (s *InMemoryCreditGrantStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	cg, exists := s.creditGrants[id]
	if !exists {
		return ierr.NewError("credit grant not found").
			WithHint("No credit grant found with the given ID").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrNotFound)
	}

	// Soft delete by setting status to archived
	cg.Status = types.StatusArchived
	s.creditGrants[id] = cg

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

// matchesFilter checks if a credit grant matches the given filter
func (s *InMemoryCreditGrantStore) matchesFilter(cg *creditgrant.CreditGrant, filter *types.CreditGrantFilter) bool {
	if filter == nil {
		return true
	}

	// Check status filter
	if filter.QueryFilter != nil && filter.QueryFilter.Status != nil {
		if cg.Status != *filter.QueryFilter.Status {
			return false
		}
	}

	// Check plan IDs filter
	if len(filter.PlanIDs) > 0 {
		if cg.PlanID == nil || !lo.Contains(filter.PlanIDs, *cg.PlanID) {
			return false
		}
	}

	// Check subscription IDs filter
	if len(filter.SubscriptionIDs) > 0 {
		if cg.SubscriptionID == nil || !lo.Contains(filter.SubscriptionIDs, *cg.SubscriptionID) {
			return false
		}
	}

	// Note: Scope and Cadence filtering would need to be added to CreditGrantFilter type if needed

	return true
}

// Clear removes all credit grants from the store
func (s *InMemoryCreditGrantStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creditGrants = make(map[string]*creditgrant.CreditGrant)
}
