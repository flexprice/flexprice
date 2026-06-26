package checkout

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the persistence interface for CheckoutSession.
type Repository interface {
	// Create inserts a new session. Returns ierr.ErrAlreadyExists if the active
	// idempotency key index fires (same key still in initiated|pending state).
	Create(ctx context.Context, session *CheckoutSession) error

	// Get retrieves a checkout session by ID.
	Get(ctx context.Context, id string) (*CheckoutSession, error)

	// Update writes all mutable fields in a single SQL UPDATE. No transaction.
	Update(ctx context.Context, session *CheckoutSession) error

	// List returns checkout sessions matching the provided filter.
	List(ctx context.Context, filter *types.CheckoutSessionFilter) ([]*CheckoutSession, error)

	// Count returns the number of sessions matching the filter.
	Count(ctx context.Context, filter *types.CheckoutSessionFilter) (int, error)

	// GetByIdempotencyKey returns the active (initiated|pending) session for the key.
	// tenant_id and environment_id are extracted from ctx.
	// Returns ierr.ErrNotFound if no active session exists.
	GetByIdempotencyKey(ctx context.Context, key string) (*CheckoutSession, error)

	// Delete soft-deletes a checkout session by setting status to archived.
	Delete(ctx context.Context, id string) error

	// MarkCompleted atomically transitions the session from pending/initiated to completed.
	// Returns (true, nil) if this call claimed the transition.
	// Returns (false, nil) if the session was already in a terminal state — idempotent no-op.
	// Never returns an error for the already-terminal case.
	MarkCompleted(ctx context.Context, sessionID string, completedAt time.Time, providerResult *types.CheckoutProviderResult) (bool, error)

	// ListExpired returns active (initiated|pending) sessions whose ExpiresAt is before
	// effectiveDate within the tenant+environment in ctx, ordered by expires_at asc.
	ListExpired(ctx context.Context, effectiveDate time.Time, limit, offset int) ([]*CheckoutSession, error)
}
