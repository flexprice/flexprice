package group

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for group data operations
type Repository interface {
	Create(ctx context.Context, group *Group) error
	Get(ctx context.Context, id string) (*Group, error)
	GetByLookupKey(ctx context.Context, lookupKey string) (*Group, error)
	Update(ctx context.Context, group *Group) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, filter *types.GroupFilter) ([]*Group, error)
	Count(ctx context.Context, filter *types.GroupFilter) (int, error)
}
