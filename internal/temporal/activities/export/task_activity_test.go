package export

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

type memTaskRepo struct {
	task.Repository
	row     *task.Task
	updated *task.Task
}

func (r *memTaskRepo) Get(context.Context, string) (*task.Task, error) { return r.row, nil }

func (r *memTaskRepo) Update(_ context.Context, t *task.Task) error {
	r.updated = t
	return nil
}

func TestCompleteTask_MergesRunMetadata(t *testing.T) {
	repo := &memTaskRepo{row: &task.Task{Metadata: map[string]interface{}{"start_time": "s"}}}
	a := NewTaskActivity(repo, logger.NewNoopLogger())

	err := a.CompleteTask(context.Background(), CompleteTaskInput{
		TaskID:   "t1",
		Metadata: map[string]string{"view_updated_at": "2026-11-01T10:00:00Z"},
	})

	require.NoError(t, err)
	require.Equal(t, "s", repo.updated.Metadata["start_time"])
	require.Equal(t, "2026-11-01T10:00:00Z", repo.updated.Metadata["view_updated_at"])
}

func TestUpdateTaskStatus_FailedSkipsCompletedRow(t *testing.T) {
	repo := &memTaskRepo{row: &task.Task{TaskStatus: types.TaskStatusCompleted}}
	a := NewTaskActivity(repo, logger.NewNoopLogger())

	err := a.UpdateTaskStatus(context.Background(), UpdateTaskStatusInput{TaskID: "t1", Status: types.TaskStatusFailed, Error: "x"})

	require.NoError(t, err)
	require.Nil(t, repo.updated)
}

func TestUpdateTaskStatus_FailedUpdatesProcessingRow(t *testing.T) {
	repo := &memTaskRepo{row: &task.Task{TaskStatus: types.TaskStatusProcessing}}
	a := NewTaskActivity(repo, logger.NewNoopLogger())

	err := a.UpdateTaskStatus(context.Background(), UpdateTaskStatusInput{TaskID: "t1", Status: types.TaskStatusFailed, Error: "x"})

	require.NoError(t, err)
	require.Equal(t, types.TaskStatusFailed, repo.updated.TaskStatus)
}

func TestCompleteTask_NilMetadataMap(t *testing.T) {
	repo := &memTaskRepo{row: &task.Task{}}
	a := NewTaskActivity(repo, logger.NewNoopLogger())

	err := a.CompleteTask(context.Background(), CompleteTaskInput{
		TaskID:   "t1",
		Metadata: map[string]string{"k": "v"},
	})

	require.NoError(t, err)
	require.Equal(t, "v", repo.updated.Metadata["k"])
}
