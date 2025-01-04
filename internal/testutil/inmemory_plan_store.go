package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryPlanStore implements plan.Repository
type InMemoryPlanStore struct {
	mu    sync.RWMutex
	plans map[string]*plan.Plan
}

func NewInMemoryPlanStore() *InMemoryPlanStore {
	return &InMemoryPlanStore{
		plans: make(map[string]*plan.Plan),
	}
}

func (s *InMemoryPlanStore) Create(ctx context.Context, p *plan.Plan) error {
	if p == nil {
		return fmt.Errorf("plan cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.plans[p.ID]; exists {
		return fmt.Errorf("plan already exists")
	}

	s.plans[p.ID] = p
	return nil
}

func (s *InMemoryPlanStore) Get(ctx context.Context, id string) (*plan.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p, exists := s.plans[id]; exists {
		return p, nil
	}
	return nil, fmt.Errorf("plan not found")
}

func (s *InMemoryPlanStore) List(ctx context.Context, filter types.Filter) ([]*plan.Plan, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*plan.Plan
	for _, p := range s.plans {
		result = append(result, p)
	}

	// Sort by created date desc (default)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	// Apply pagination
	start := filter.Offset
	if start >= len(result) {
		return []*plan.Plan{}, nil
	}

	end := start + filter.Limit
	if end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

func (s *InMemoryPlanStore) Update(ctx context.Context, p *plan.Plan) error {
	if p == nil {
		return fmt.Errorf("plan cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.plans[p.ID]; !exists {
		return fmt.Errorf("plan not found")
	}

	s.plans[p.ID] = p
	return nil
}

func (s *InMemoryPlanStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.plans[id]; !exists {
		return fmt.Errorf("plan not found")
	}

	delete(s.plans, id)
	return nil
}

func (s *InMemoryPlanStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.plans = make(map[string]*plan.Plan)
}
