package commitment

import (
	"context"
)

// Repository is the storage surface for commitments.
type Repository interface {
	Create(ctx context.Context, c *Commitment) (*Commitment, error)
	Get(ctx context.Context, id string) (*Commitment, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*Commitment, error)
	Update(ctx context.Context, c *Commitment) (*Commitment, error)
	// Delete archives the commitment, which frees its idempotency key.
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *Filter) ([]*Commitment, error)
	Count(ctx context.Context, filter *Filter) (int, error)
}
