package testutil

import (
	"context"
	"errors"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryUserStore is an in-memory implementation of the User repository
type InMemoryUserStore struct {
	mu    sync.Mutex
	users map[string]*user.User
}

// NewInMemoryUserStore creates a new instance of InMemoryUserStore
func NewInMemoryUserStore() *InMemoryUserStore {
	return &InMemoryUserStore{
		users: make(map[string]*user.User),
	}
}

// Create creates a new user in the in-memory store
func (r *InMemoryUserStore) Create(ctx context.Context, user *user.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[user.Email]; exists {
		return errors.New("user already exists")
	}

	r.users[user.Email] = user
	return nil
}

// GetByEmail retrieves a user by email from the in-memory store
func (r *InMemoryUserStore) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, exists := r.users[email]
	if !exists {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// GetByID retrieves a user by ID from the in-memory store
func (r *InMemoryUserStore) GetByID(ctx context.Context, userID string) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, u := range r.users {
		if u.ID == userID {
			return u, nil
		}
	}
	return nil, errors.New("user not found")
}

// ListByFilter is a minimal implementation for testing
func (r *InMemoryUserStore) ListByFilter(ctx context.Context, filter *types.UserFilter) ([]*user.User, int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get tenant ID from context
	tenantID, ok := ctx.Value(types.CtxTenantID).(string)
	if !ok {
		return nil, 0, errors.New("tenant ID not found in context")
	}

	var result []*user.User
	for _, u := range r.users {
		if u.TenantID != tenantID {
			continue
		}
		
		// Filter by type if specified
		if filter.Type != nil && u.Type != *filter.Type {
			continue
		}
		
		result = append(result, u)
	}
	
	return result, int64(len(result)), nil
}

func (s *InMemoryUserStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.users = make(map[string]*user.User)
}
