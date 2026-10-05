package activitylog

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

type Repository interface {
	List(ctx context.Context, f *types.ActivityFilter) ([]*ActivityLog, *types.ActivityCursor, error)
	Get(ctx context.Context, id string) (*ActivityLog, error)
}
