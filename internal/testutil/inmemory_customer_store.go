// In-memory customer repository for testing
package testutil

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryCustomerStore implements customer.Repository
type InMemoryCustomerStore struct {
	mu        sync.RWMutex
	customers map[string]*customer.Customer
}

func NewInMemoryCustomerStore() *InMemoryCustomerStore {
	return &InMemoryCustomerStore{
		customers: make(map[string]*customer.Customer),
	}
}

func (s *InMemoryCustomerStore) Create(ctx context.Context, c *customer.Customer) error {
	if c == nil {
		return fmt.Errorf("customer cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.customers[c.ID]; exists {
		return fmt.Errorf("customer already exists")
	}

	s.customers[c.ID] = c
	return nil
}

func (s *InMemoryCustomerStore) Get(ctx context.Context, id string) (*customer.Customer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if c, exists := s.customers[id]; exists {
		return c, nil
	}
	return nil, fmt.Errorf("customer not found")
}

func (s *InMemoryCustomerStore) List(ctx context.Context, filter types.Filter) ([]*customer.Customer, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*customer.Customer
	for _, c := range s.customers {
		result = append(result, c)
	}

	// Sort by created date desc (default)
	sort.Slice(result, func(i, j int) bool {
		return result[i].CreatedAt.After(result[j].CreatedAt)
	})

	// Apply pagination
	start := filter.Offset
	if start >= len(result) {
		return []*customer.Customer{}, nil
	}

	end := start + filter.Limit
	if end > len(result) {
		end = len(result)
	}

	return result[start:end], nil
}

func (s *InMemoryCustomerStore) Update(ctx context.Context, c *customer.Customer) error {
	if c == nil {
		return fmt.Errorf("customer cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.customers[c.ID]; !exists {
		return fmt.Errorf("customer not found")
	}

	s.customers[c.ID] = c
	return nil
}

func (s *InMemoryCustomerStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.customers[id]; !exists {
		return fmt.Errorf("customer not found")
	}

	delete(s.customers, id)
	return nil
}

func (s *InMemoryCustomerStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.customers = make(map[string]*customer.Customer)
}
