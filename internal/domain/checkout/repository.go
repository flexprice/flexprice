package checkout

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository persists Checkout aggregates.
type Repository interface {
	Create(ctx context.Context, c *Checkout) error
	Get(ctx context.Context, id string) (*Checkout, error)
	Update(ctx context.Context, c *Checkout) error

	// GetPendingByEntity returns the single pending checkout for a subject and
	// objective (used by payment completion + dedupe). Returns nil, nil if none.
	GetPendingByEntity(ctx context.Context, entityType types.CheckoutEntityType,
		entityID string, objective types.CheckoutObjective) (*Checkout, error)

	// GetPendingBySourceSubscription returns the single pending checkout whose
	// source_subscription_id matches (used to dedupe change-checkouts). Returns nil, nil if none.
	GetPendingBySourceSubscription(ctx context.Context, sourceSubscriptionID string) (*Checkout, error)

	// ListPendingExpired returns pending checkouts whose expires_at < cutoff.
	ListPendingExpired(ctx context.Context, cutoff time.Time) ([]*Checkout, error)
}
