package task

import (
	"context"

	"github.com/flexprice/flexprice/internal/types"
)

// Repository defines the interface for task operations
type Repository interface {
	// Standard CRUD operations
	Create(ctx context.Context, task *Task) error
	Get(ctx context.Context, id string) (*Task, error)
	List(ctx context.Context, filter *types.TaskFilter) ([]*Task, error)
	Count(ctx context.Context, filter *types.TaskFilter) (int, error)
	Update(ctx context.Context, task *Task) error
	Delete(ctx context.Context, id string) error
}
