package refund

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the persistence interface for Refund entities.
type Repository interface {
	Create(ctx context.Context, refund *Refund) error
	Get(ctx context.Context, id string) (*Refund, error)
	Update(ctx context.Context, refund *Refund) error

	// Delete soft-deletes a refund (sets status to archived).
	Delete(ctx context.Context, id string) error

	List(ctx context.Context, filter *types.RefundFilter) ([]*Refund, error)
	Count(ctx context.Context, filter *types.RefundFilter) (int, error)

	// GetByIdempotencyKey looks up a refund by (tenant_id, environment_id, idempotency_key).
	GetByIdempotencyKey(ctx context.Context, key string) (*Refund, error)
}
