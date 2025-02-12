package feature

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	Create(ctx context.Context, feature *Feature) error
	Get(ctx context.Context, id string) (*Feature, error)
	List(ctx context.Context, filter *types.FeatureFilter) ([]*Feature, error)
	ListAll(ctx context.Context, filter *types.FeatureFilter) ([]*Feature, error)
	Count(ctx context.Context, filter *types.FeatureFilter) (int, error)
	Update(ctx context.Context, feature *Feature) error
	Delete(ctx context.Context, id string) error
}
