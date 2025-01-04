package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/tenant"
)

type InMemoryTenantStore struct {
	mu      sync.RWMutex
	tenants map[string]*tenant.Tenant
}

func NewInMemoryTenantStore() *InMemoryTenantStore {
	return &InMemoryTenantStore{
		tenants: make(map[string]*tenant.Tenant),
	}
}

func (s *InMemoryTenantStore) Create(ctx context.Context, t *tenant.Tenant) error {
	if t == nil {
		return fmt.Errorf("tenant cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tenants[t.ID]; exists {
		return fmt.Errorf("tenant already exists")
	}

	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	s.tenants[t.ID] = t
	return nil
}

func (s *InMemoryTenantStore) GetByID(ctx context.Context, id string) (*tenant.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if t, exists := s.tenants[id]; exists {
		return t, nil
	}
	return nil, fmt.Errorf("tenant not found")
}

func (s *InMemoryTenantStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tenants = make(map[string]*tenant.Tenant)
}
