package export

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

type memTaskRepo struct {
	task.Repository
	row     *task.Task
	updated *task.Task
	// Status in the writer; row may be a stale replica read.
	writerStatus types.TaskStatus
}

func (r *memTaskRepo) Get(context.Context, string) (*task.Task, error) { return r.row, nil }

func (r *memTaskRepo) UpdateUnlessCompleted(_ context.Context, t *task.Task) (bool, error) {
	if r.writerStatus == types.TaskStatusCompleted {
		return false, nil
	}
	r.updated = t
	return true, nil
}

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

func TestUpdateTaskStatus_FailedRefusedByCompletedRow(t *testing.T) {
	for name, readStatus := range map[string]types.TaskStatus{
		"fresh read":         types.TaskStatusCompleted,
		"stale replica read": types.TaskStatusProcessing,
	} {
		t.Run(name, func(t *testing.T) {
			repo := &memTaskRepo{row: &task.Task{TaskStatus: readStatus}, writerStatus: types.TaskStatusCompleted}
			a := NewTaskActivity(repo, logger.NewNoopLogger())

			err := a.UpdateTaskStatus(context.Background(), UpdateTaskStatusInput{TaskID: "t1", Status: types.TaskStatusFailed, Error: "x"})

			var appErr *temporal.ApplicationError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, TaskAlreadyCompletedErrType, appErr.Type())
			require.True(t, appErr.NonRetryable())
			require.Nil(t, repo.updated)
		})
	}
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
