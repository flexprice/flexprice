package testutil

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/task"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryTaskStore implements task.Repository
type InMemoryTaskStore struct {
	*InMemoryStore[*task.Task]
}

// NewInMemoryTaskStore creates a new in-memory task store
func NewInMemoryTaskStore() *InMemoryTaskStore {
	return &InMemoryTaskStore{
		InMemoryStore: NewInMemoryStore[*task.Task](),
	}
}

// Helper to copy task
func copyTask(t *task.Task) *task.Task {
	if t == nil {
		return nil
	}

	return &task.Task{
		ID:                t.ID,
		TaskType:          t.TaskType,
		EntityType:        t.EntityType,
		ScheduledTaskID:   t.ScheduledTaskID,
		FileURL:           t.FileURL,
		FileType:          t.FileType,
		TaskStatus:        t.TaskStatus,
		TotalRecords:      t.TotalRecords,
		ProcessedRecords:  t.ProcessedRecords,
		SuccessfulRecords: t.SuccessfulRecords,
		FailedRecords:     t.FailedRecords,
		ErrorSummary:      t.ErrorSummary,
		Metadata:          t.Metadata,
		StartedAt:         t.StartedAt,
		CompletedAt:       t.CompletedAt,
		FailedAt:          t.FailedAt,
		EnvironmentID:     t.EnvironmentID,
		BaseModel:         t.BaseModel,
	}
}

func (s *InMemoryTaskStore) Create(ctx context.Context, t *task.Task) error {
	if t == nil {
		return ierr.NewError("task cannot be nil").
			WithHint("Task cannot be nil").
			Mark(ierr.ErrValidation)
	}

	// Set environment ID from context if not already set
	if t.EnvironmentID == "" {
		t.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	return s.InMemoryStore.Create(ctx, t.ID, copyTask(t))
}

func (s *InMemoryTaskStore) Get(ctx context.Context, id string) (*task.Task, error) {
	t, err := s.InMemoryStore.Get(ctx, id)
	if err != nil {
		return nil, ierr.NewError("task not found").
			WithHint("Task not found").
			WithReportableDetails(map[string]interface{}{
				"id": id,
			}).
			Mark(ierr.ErrNotFound)
	}
	return copyTask(t), nil
}

func (s *InMemoryTaskStore) Update(ctx context.Context, t *task.Task) error {
	if t == nil {
		return ierr.NewError("task cannot be nil").
			WithHint("Task cannot be nil").
			Mark(ierr.ErrValidation)
	}
	return s.InMemoryStore.Update(ctx, t.ID, copyTask(t))
}

func (s *InMemoryTaskStore) Delete(ctx context.Context, id string) error {
	return s.InMemoryStore.Delete(ctx, id)
}

func (s *InMemoryTaskStore) List(ctx context.Context, filter *types.TaskFilter) ([]*task.Task, error) {
	return s.InMemoryStore.List(ctx, filter, taskFilterFn, taskSortFn)
}

func (s *InMemoryTaskStore) Count(ctx context.Context, filter *types.TaskFilter) (int, error) {
	return s.InMemoryStore.Count(ctx, filter, taskFilterFn)
}

// GetLastExportTask gets the last export task for a scheduled job (regardless of status)
func (s *InMemoryTaskStore) GetLastExportTask(ctx context.Context, scheduledJobID string) (*task.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var lastTask *task.Task
	for _, t := range s.items {
		if t.ScheduledTaskID == scheduledJobID &&
			t.TaskType == types.TaskTypeExport &&
			t.Status == types.StatusPublished &&
			CheckEnvironmentFilter(ctx, t.EnvironmentID) &&
			t.TenantID == types.GetTenantID(ctx) {

			if lastTask == nil || t.CreatedAt.After(lastTask.CreatedAt) {
				lastTask = t
			}
		}
	}

	if lastTask == nil {
		return nil, nil // No previous task found - not an error
	}

	return copyTask(lastTask), nil
}

// taskFilterFn implements filtering logic for tasks
func taskFilterFn(ctx context.Context, t *task.Task, filter interface{}) bool {
	if t == nil {
		return false
	}

	f, ok := filter.(*types.TaskFilter)
	if !ok {
		return true // No filter applied
	}

	// Check tenant ID
	if tenantID, ok := ctx.Value(types.CtxTenantID).(string); ok {
		if t.TenantID != tenantID {
			return false
		}
	}

	// Apply environment filter
	if !CheckEnvironmentFilter(ctx, t.EnvironmentID) {
		return false
	}

	// Filter by task type
	if f.TaskType != nil && t.TaskType != *f.TaskType {
		return false
	}

	// Filter by entity type
	if f.EntityType != nil && t.EntityType != *f.EntityType {
		return false
	}

	// Filter by task status
	if f.TaskStatus != nil && t.TaskStatus != *f.TaskStatus {
		return false
	}

	// Filter by scheduled task ID
	if f.ScheduledTaskID != "" && t.ScheduledTaskID != f.ScheduledTaskID {
		return false
	}

	// Filter by created by
	if f.CreatedBy != "" && t.CreatedBy != f.CreatedBy {
		return false
	}

	// Filter by status
	if f.Status != nil && t.Status != *f.Status {
		return false
	}

	// Filter by time range
	if f.StartTime != nil && t.CreatedAt.Before(*f.StartTime) {
		return false
	}
	if f.EndTime != nil && t.CreatedAt.After(*f.EndTime) {
		return false
	}

	return true
}

// taskSortFn implements sorting logic for tasks
func taskSortFn(i, j *task.Task) bool {
	if i == nil || j == nil {
		return false
	}
	return i.CreatedAt.After(j.CreatedAt)
}
