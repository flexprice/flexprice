package testutil

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/types"
)

type InMemoryEnvironmentRepository struct {
	mu           sync.RWMutex
	environments map[string]*environment.Environment
}

func NewInMemoryEnvironmentStore() *InMemoryEnvironmentRepository {
	return &InMemoryEnvironmentRepository{
		environments: make(map[string]*environment.Environment),
	}
}

func (s *InMemoryEnvironmentRepository) Create(ctx context.Context, env *environment.Environment) error {
	if env == nil {
		return fmt.Errorf("environment cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.environments[env.ID]; exists {
		return fmt.Errorf("environment already exists")
	}

	env.CreatedAt = time.Now()
	env.UpdatedAt = time.Now()
	s.environments[env.ID] = env
	return nil
}

func (s *InMemoryEnvironmentRepository) Get(ctx context.Context, id string) (*environment.Environment, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if env, exists := s.environments[id]; exists {
		return env, nil
	}
	return nil, fmt.Errorf("environment not found")
}

func (s *InMemoryEnvironmentRepository) List(ctx context.Context, filter types.Filter) ([]*environment.Environment, error) {
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

func (s *InMemoryEnvironmentRepository) Update(ctx context.Context, env *environment.Environment) error {
	if env == nil {
		return fmt.Errorf("environment cannot be nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	existing, exists := s.environments[env.ID]
	if !exists {
		return fmt.Errorf("environment not found")
	}

	existing.Name = env.Name
	existing.Type = env.Type
	existing.Slug = env.Slug
	existing.UpdatedAt = time.Now()
	existing.UpdatedBy = env.UpdatedBy
	return nil
}
