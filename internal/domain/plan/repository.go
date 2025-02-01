package plan

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for plan persistence
type Repository interface {
	Create(ctx context.Context, plan *Plan) error
	Get(ctx context.Context, id string) (*Plan, error)
	List(ctx context.Context, filter *types.PlanFilter) ([]*Plan, error)
	ListAll(ctx context.Context, filter *types.PlanFilter) ([]*Plan, error)
	Count(ctx context.Context, filter *types.PlanFilter) (int, error)
	Update(ctx context.Context, plan *Plan) error
	Delete(ctx context.Context, id string) error
}
