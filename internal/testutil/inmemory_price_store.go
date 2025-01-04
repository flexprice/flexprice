package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryPriceStore implements price.Repository
type InMemoryPriceStore struct {
	mu     sync.RWMutex
	prices map[string]*price.Price
}

func NewInMemoryPriceStore() *InMemoryPriceStore {
	return &InMemoryPriceStore{
		prices: make(map[string]*price.Price),
	}
}

func (s *InMemoryPriceStore) Create(ctx context.Context, p *price.Price) error {
	if p == nil {
		return fmt.Errorf("price cannot be nil")
	}

	// Set TenantID from the context
	tenantID, _ := ctx.Value(types.CtxTenantID).(string)
	p.TenantID = tenantID

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.prices[p.ID]; exists {
		return fmt.Errorf("price already exists")
	}

	s.prices[p.ID] = p
	return nil
}

func (s *InMemoryPriceStore) Get(ctx context.Context, id string) (*price.Price, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if p, exists := s.prices[id]; exists {
		return p, nil
	}
	return nil, fmt.Errorf("price not found")
}

func (s *InMemoryPriceStore) GetByPlanID(ctx context.Context, planID string) ([]*price.Price, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenantID, _ := ctx.Value(types.CtxTenantID).(string)
	var result []*price.Price
	for _, p := range s.prices {
		if p.PlanID == planID && p.TenantID == tenantID {
			result = append(result, p)
		}
	}

	// Sort by created date desc
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	return result, nil
}

func (s *InMemoryPriceStore) List(ctx context.Context, filter types.Filter) ([]*price.Price, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*price.Price
	for _, p := range s.prices {
		result = append(result, p)
	}

	// Sort by created date desc
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	// Apply pagination
	start := filter.Offset
	if start >= len(result) {
		return []*price.Price{}, nil
	}

	end := start + filter.Limit
	if end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

func (s *InMemoryPriceStore) Update(ctx context.Context, p *price.Price) error {
	if p == nil {
		return fmt.Errorf("price cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.prices[p.ID]; !exists {
		return fmt.Errorf("price not found")
	}

	s.prices[p.ID] = p
	return nil
}

func (s *InMemoryPriceStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.prices[id]; !exists {
		return fmt.Errorf("price not found")
	}

	delete(s.prices, id)
	return nil
}

func (s *InMemoryPriceStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.prices = make(map[string]*price.Price)
}
