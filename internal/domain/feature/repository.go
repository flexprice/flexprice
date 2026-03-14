package feature

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for feature storage operations
type Repository interface {
	Create(ctx context.Context, feature *Feature) error
	Get(ctx context.Context, id string) (*Feature, error)
	List(ctx context.Context, filter *types.FeatureFilter) ([]*Feature, error)
	ListAll(ctx context.Context, filter *types.FeatureFilter) ([]*Feature, error)
	Count(ctx context.Context, filter *types.FeatureFilter) (int, error)
	Update(ctx context.Context, feature *Feature) error
	Delete(ctx context.Context, id string) error
	ListByIDs(ctx context.Context, featureIDs []string) ([]*Feature, error)

	// Group-related operations
	GetByGroupIDs(ctx context.Context, groupIDs []string) ([]*Feature, error)
	ClearByGroupID(ctx context.Context, groupID string) error
}
