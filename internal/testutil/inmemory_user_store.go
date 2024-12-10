package testutil

import (
	"context"
	"errors"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/user"
)

// InMemoryUserRepository is an in-memory implementation of the User repository
type InMemoryUserRepository struct {
	mu    sync.Mutex
	users map[string]*user.User
}

// NewInMemoryUserRepository creates a new instance of InMemoryUserRepository
func NewInMemoryUserRepository() *InMemoryUserRepository {
	return &InMemoryUserRepository{
		users: make(map[string]*user.User),
	}
}

// Create creates a new user in the in-memory store
func (r *InMemoryUserRepository) Create(ctx context.Context, user *user.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[user.Email]; exists {
		return errors.New("user already exists")
	}

	r.users[user.Email] = user
	return nil
}

// GetByEmail retrieves a user by email from the in-memory store
func (r *InMemoryUserRepository) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	user, exists := r.users[email]
	if !exists {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// GetByID retrieves a user by ID from the in-memory store
func (r *InMemoryUserRepository) GetByID(ctx context.Context, userID string) (*user.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, u := range r.users {
		if u.ID == userID {
			return u, nil
		}
	}
	return nil, errors.New("user not found")
}
