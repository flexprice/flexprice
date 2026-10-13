package commitment

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository is the storage surface for commitments.
type Repository interface {
	Create(ctx context.Context, c *Commitment) (*Commitment, error)
	Get(ctx context.Context, id string) (*Commitment, error)
	GetByIdempotencyKey(ctx context.Context, key string) (*Commitment, error)
	Update(ctx context.Context, c *Commitment) (*Commitment, error)
	List(ctx context.Context, filter *types.CommitmentFilter) ([]*Commitment, error)
	Count(ctx context.Context, filter *types.CommitmentFilter) (int, error)
}
