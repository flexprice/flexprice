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
	// ListStaleExportTasks lists unfinished export runs created before olderThan, across all
	// tenants, oldest first.
	ListStaleExportTasks(ctx context.Context, olderThan time.Time, limit int) ([]StaleRef, error)
	// FailStaleExportTask fails one unfinished run within the tenant and environment in ctx.
	// It reports whether a row changed.
	FailStaleExportTask(ctx context.Context, id string, reason string) (bool, error)
}

// StaleRef identifies a stale run and its scope.
type StaleRef struct {
	ID            string
	TenantID      string
	EnvironmentID string
}
