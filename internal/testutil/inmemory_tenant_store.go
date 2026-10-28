package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
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
		return ierr.NewError("tenant cannot be nil").
			WithHint("Please provide a valid tenant").
			Mark(ierr.ErrValidation)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tenants[t.ID]; exists {
		return ierr.NewError("tenant already exists").
			WithHint("Please provide a unique tenant ID").
			Mark(ierr.ErrDatabase)
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
	return nil, ierr.NewError("tenant not found").
		WithHint("Please provide a valid tenant ID").
		Mark(ierr.ErrDatabase)
}

func (s *InMemoryTenantStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.tenants = make(map[string]*tenant.Tenant)
}

func (s *InMemoryTenantStore) List(ctx context.Context) ([]*tenant.Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenants := make([]*tenant.Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		tenants = append(tenants, t)
	}
	return tenants, nil
}

func (s *InMemoryTenantStore) Update(ctx context.Context, t *tenant.Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tenants[t.ID]; !exists {
		return ierr.NewError("tenant not found").
			WithHint("Please provide a valid tenant ID").
			Mark(ierr.ErrDatabase)
	}

	s.tenants[t.ID] = t
	return nil
}
