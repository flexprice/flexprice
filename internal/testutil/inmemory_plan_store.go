package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/plan"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryPlanStore implements plan.Repository
type InMemoryPlanStore struct {
	*InMemoryStore[*plan.Plan]
}

// NewInMemoryPlanStore creates a new in-memory plan store
func NewInMemoryPlanStore() *InMemoryPlanStore {
	return &InMemoryPlanStore{
		InMemoryStore: NewInMemoryStore[*plan.Plan](),
	}
}

// planFilterFn implements filtering logic for plans
func planFilterFn(ctx context.Context, p *plan.Plan, filter interface{}) bool {
	if p == nil {
		return false
	}

	f, ok := filter.(*types.PlanFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if p.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, p.EnvironmentID) {
		return false
	}

	// Filter by status
	if f.Status != nil && p.Status != *f.Status {
		return false
	}

	// Filter by time range
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil && p.CreatedAt.Before(*f.StartTime) {
			return false
		}
		if f.EndTime != nil && p.CreatedAt.After(*f.EndTime) {
			return false
		}
	}

	return true
}

// planSortFn implements sorting logic for plans
func planSortFn(i, j *plan.Plan) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}

func (s *InMemoryPlanStore) Create(ctx context.Context, p *plan.Plan) error {
	if p == nil {
		return ierr.NewError("plan cannot be nil").
			WithHint("Plan object is required").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if p.EnvironmentID == "" {
		p.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, p.ID, p)
}

func (s *InMemoryPlanStore) Get(ctx context.Context, id string) (*plan.Plan, error) {
	return s.InMemoryStore.Get(ctx, id)
}

func (s *InMemoryPlanStore) List(ctx context.Context, filter *types.PlanFilter) ([]*plan.Plan, error) {
	return s.InMemoryStore.List(ctx, filter, planFilterFn, planSortFn)
}

func (s *InMemoryPlanStore) Count(ctx context.Context, filter *types.PlanFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, planFilterFn)
}

func (s *InMemoryPlanStore) Update(ctx context.Context, p *plan.Plan) error {
	if p == nil {
		return ierr.NewError("plan cannot be nil").
			WithHint("Plan object is required").
			Mark(ierr.ErrValidation)
	}
	return s.InMemoryStore.Update(ctx, p.ID, p)
}

func (s *InMemoryPlanStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

// ListAll returns all plans without pagination
func (s *InMemoryPlanStore) ListAll(ctx context.Context, filter *types.PlanFilter) ([]*plan.Plan, error) {
	// Create an unlimited filter
	unlimitedFilter := &types.PlanFilter{
		QueryFilter:     types.NewNoLimitQueryFilter(),
		TimeRangeFilter: filter.TimeRangeFilter,
	}

	return s.List(ctx, unlimitedFilter)
}

// GetByLookupKey retrieves a plan by its lookup key
func (s *InMemoryPlanStore) GetByLookupKey(ctx context.Context, lookupKey string) (*plan.Plan, error) {
	plans, err := s.List(ctx, &types.PlanFilter{
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	if err != nil {
		return nil, err
	}

	for _, p := range plans {
		if p.LookupKey == lookupKey &&
			p.Status == types.StatusPublished &&
			CheckEnvironmentFilter(ctx, p.EnvironmentID) {
			return p, nil
		}
	}

	return nil, ierr.NewError("plan not found").
		WithHintf("Plan with lookup key %s was not found", lookupKey).
		WithReportableDetails(map[string]any{
			"lookup_key": lookupKey,
		}).
		Mark(ierr.ErrNotFound)
}

// Clear clears the plan store
func (s *InMemoryPlanStore) Clear() {
	s.InMemoryStore.Clear()
}
