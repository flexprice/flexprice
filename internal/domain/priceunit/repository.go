package priceunit

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Get(ctx context.Context, id string) (*PriceUnit, error)
	List(ctx context.Context, filter *types.PriceUnitFilter) ([]*PriceUnit, error)
	Create(ctx context.Context, priceUnit *PriceUnit) (*PriceUnit, error)
	Update(ctx context.Context, priceUnit *PriceUnit) (*PriceUnit, error)
	Delete(ctx context.Context, priceUnit *PriceUnit) error

	GetByCode(ctx context.Context, code string) (*PriceUnit, error)
}
