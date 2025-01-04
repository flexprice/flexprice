package testutil

import (
	"context"
	"errors"
	"sync"

	"github.com/flexprice/flexprice/internal/domain/auth"
)

// InMemoryAuthRepository is an in-memory implementation of the auth.Repository interface
type InMemoryAuthRepository struct {
	mu    sync.Mutex
	auths map[string]*auth.Auth
}

// NewInMemoryAuthRepository creates a new instance of InMemoryAuthRepository
func NewInMemoryAuthRepository() *InMemoryAuthRepository {
	return &InMemoryAuthRepository{
		auths: make(map[string]*auth.Auth),
	}
}

// CreateAuth creates a new auth record in the in-memory store
func (r *InMemoryAuthRepository) CreateAuth(ctx context.Context, auth *auth.Auth) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.auths[auth.UserID] = auth
	return nil
}

// UpdateAuth updates an existing auth record in the in-memory store
func (r *InMemoryAuthRepository) UpdateAuth(ctx context.Context, auth *auth.Auth) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.auths[auth.UserID]; !exists {
		return errors.New("auth record not found")
	}

	r.auths[auth.UserID] = auth
	return nil
}

// GetAuthByUserID retrieves an auth record by user ID from the in-memory store
func (r *InMemoryAuthRepository) GetAuthByUserID(ctx context.Context, userID string) (*auth.Auth, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	auth, exists := r.auths[userID]
	if !exists {
		return nil, errors.New("auth record not found")
	}

	return auth, nil
}

// DeleteAuth deletes an auth record by user ID from the in-memory store
func (r *InMemoryAuthRepository) DeleteAuth(ctx context.Context, userID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.auths, userID)
	return nil
}

// Clear clears all auth records from the in-memory store
func (r *InMemoryAuthRepository) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.auths = make(map[string]*auth.Auth)
}
