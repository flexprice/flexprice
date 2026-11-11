package task

import (
	"context"
	"time"

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
	// SetTaskMetadataKey writes one metadata key on the writer.
	SetTaskMetadataKey(ctx context.Context, id, key string, value any) error
	// FailStaleExportTasks fails unfinished export runs created before olderThan, across all
	// tenants. It returns the number updated, at most limit.
	FailStaleExportTasks(ctx context.Context, olderThan time.Time, reason string, limit int) (int, error)
}
