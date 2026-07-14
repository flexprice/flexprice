package refund

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the persistence interface for Refund entities.
type Repository interface {
	// Create persists a new refund.
	Create(ctx context.Context, refund *Refund) error

	// Get fetches a refund by its primary key.
	Get(ctx context.Context, id string) (*Refund, error)

	// Update persists changes to an existing refund.
	Update(ctx context.Context, refund *Refund) error

	// Delete soft-deletes a refund (sets status to archived).
	Delete(ctx context.Context, id string) error

	// List returns refunds matching the given filter.
	List(ctx context.Context, filter *types.RefundFilter) ([]*Refund, error)

	// Count returns the number of refunds matching the given filter.
	Count(ctx context.Context, filter *types.RefundFilter) (int, error)

	// GetByIdempotencyKey looks up a refund by (tenant_id, environment_id, idempotency_key).
	GetByIdempotencyKey(ctx context.Context, key string) (*Refund, error)
}
