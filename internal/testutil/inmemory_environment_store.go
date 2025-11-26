package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

type InMemoryEnvironmentStore struct {
	mu           sync.RWMutex
	environments map[string]*environment.Environment
}

func NewInMemoryEnvironmentStore() *InMemoryEnvironmentStore {
	return &InMemoryEnvironmentStore{
		environments: make(map[string]*environment.Environment),
	}
}

func (s *InMemoryEnvironmentStore) Create(ctx context.Context, env *environment.Environment) error {
	if env == nil {
		return ierr.NewError("environment cannot be nil").
			WithHint("Environment cannot be nil").
			Mark(ierr.ErrDatabase)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.environments[env.ID]; exists {
		return ierr.NewError("environment already exists").
			WithHint("Environment already exists").
			Mark(ierr.ErrDatabase)
	}

	env.CreatedAt = time.Now()
	env.UpdatedAt = time.Now()
	s.environments[env.ID] = env
	return nil
}

func (s *InMemoryEnvironmentStore) Get(ctx context.Context, id string) (*environment.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if env, exists := s.environments[id]; exists {
		return env, nil
	}
	return nil, ierr.NewError("environment not found").
		WithHint("Environment not found").
		Mark(ierr.ErrDatabase)
}

func (s *InMemoryEnvironmentStore) List(ctx context.Context, filter types.Filter) ([]*environment.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var environments []*environment.Environment
	offset := filter.Offset
	limit := filter.Limit

	for _, env := range s.environments {
		if offset > 0 {
			offset--
			continue
		}
		if limit == 0 {
			break
		}
		environments = append(environments, env)
		limit--
	}

	return environments, nil
}

func (s *InMemoryEnvironmentStore) Update(ctx context.Context, env *environment.Environment) error {
	if env == nil {
		return ierr.NewError("environment cannot be nil").
			WithHint("Environment cannot be nil").
			Mark(ierr.ErrDatabase)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.environments[env.ID]
	if !exists {
		return ierr.NewError("environment not found").
			WithHint("Environment not found").
			Mark(ierr.ErrDatabase)
	}

	existing.Name = env.Name
	existing.Type = env.Type
	existing.UpdatedAt = time.Now()
	existing.UpdatedBy = env.UpdatedBy
	return nil
}

func (s *InMemoryEnvironmentStore) CountByType(ctx context.Context, envType types.EnvironmentType) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenantID := types.GetTenantID(ctx)
	count := 0

	for _, env := range s.environments {
		if env.TenantID == tenantID &&
			env.Type == envType &&
			env.Status == types.StatusPublished {
			count++
		}
	}

	return count, nil
}

func (s *InMemoryEnvironmentStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.environments = make(map[string]*environment.Environment)
}
