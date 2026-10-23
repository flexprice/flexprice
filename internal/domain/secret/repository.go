package secret

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for secret data access
type Repository interface {
	// Create creates a new secret
	Create(ctx context.Context, secret *Secret) error

	// Get retrieves a secret by ID
	Get(ctx context.Context, id string) (*Secret, error)

	// List retrieves secrets based on filter criteria
	List(ctx context.Context, filter *types.SecretFilter) ([]*Secret, error)

	// Count counts secrets based on filter criteria
	Count(ctx context.Context, filter *types.SecretFilter) (int, error)

	// ListAll retrieves all secrets based on filter criteria (no pagination)
	ListAll(ctx context.Context, filter *types.SecretFilter) ([]*Secret, error)

	// Delete deletes a secret by ID
	Delete(ctx context.Context, id string) error

	// GetAPIKeyByValue retrieves an API key by value
	GetAPIKeyByValue(ctx context.Context, value string) (*Secret, error)

	// RevokeByUserID deletes every published secret owned by the user, across environments.
	// It returns the revoked secrets; call InvalidateCache after commit.
	RevokeByUserID(ctx context.Context, userID string) ([]*Secret, error)

	// InvalidateCache evicts the secrets from the auth cache
	InvalidateCache(ctx context.Context, secrets []*Secret)

	// UpdateLastUsed updates the last used timestamp of a secret
	UpdateLastUsed(ctx context.Context, id string) error
}
