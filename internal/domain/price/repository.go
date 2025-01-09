package price

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Create(ctx context.Context, price *Price) error
	Get(ctx context.Context, id string) (*Price, error)
	List(ctx context.Context, filter types.PriceFilter) ([]*Price, error)
	Update(ctx context.Context, price *Price) error
	Delete(ctx context.Context, id string) error
}
