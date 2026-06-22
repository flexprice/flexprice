package checkout

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// GetPendingByEntityParams scopes a pending-checkout lookup to a specific entity.
type GetPendingByEntityParams struct {
	EntityType types.CheckoutEntityType
	EntityID   string
	Mode       types.CheckoutObjective
}

// Repository persists Checkout aggregates.
type Repository interface {
	Create(ctx context.Context, c *Checkout) error
	Get(ctx context.Context, id string) (*Checkout, error)
	Update(ctx context.Context, c *Checkout) error

	// GetPendingByEntity returns the single pending checkout for a subject and
	// mode (used by payment completion + dedupe). Returns nil, nil if none.
	GetPendingByEntity(ctx context.Context, params GetPendingByEntityParams) (*Checkout, error)

	// ListPendingExpired returns pending checkouts whose expires_at < cutoff.
	// filter may be nil for an unlimited sweep (used by the cleanup cron).
	ListPendingExpired(ctx context.Context, cutoff time.Time, filter *types.QueryFilter) ([]*Checkout, error)
}
