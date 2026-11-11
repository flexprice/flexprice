package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/stretchr/testify/require"
)

func TestNewTaskResponse_KeepsScheduledTaskID(t *testing.T) {
	resp := NewTaskResponse(&task.Task{ID: "task_1", ScheduledTaskID: "schtask_1"})
	require.Equal(t, "schtask_1", resp.ScheduledTaskID)
}
