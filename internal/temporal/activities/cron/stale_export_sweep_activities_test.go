package cron

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

func seedTask(t *testing.T, store *testutil.InMemoryTaskStore, id string, tt types.TaskType, status types.TaskStatus, age time.Duration) {
	t.Helper()
	row := &task.Task{ID: id, TaskType: tt, TaskStatus: status, EnvironmentID: "e1"}
	row.TenantID = "t1"
	row.Status = types.StatusPublished
	row.CreatedAt = clock.Now().Add(-age)
	require.NoError(t, store.Create(context.Background(), row))
}

func TestSweepStaleExportRuns(t *testing.T) {
	store := testutil.NewInMemoryTaskStore()
	seedTask(t, store, "old_processing", types.TaskTypeExport, types.TaskStatusProcessing, 3*time.Hour)
	seedTask(t, store, "old_pending", types.TaskTypeExport, types.TaskStatusPending, 3*time.Hour)
	seedTask(t, store, "old_completed", types.TaskTypeExport, types.TaskStatusCompleted, 3*time.Hour)
	seedTask(t, store, "old_import", types.TaskTypeImport, types.TaskStatusProcessing, 3*time.Hour)
	seedTask(t, store, "recent_processing", types.TaskTypeExport, types.TaskStatusProcessing, time.Hour)

	a := NewStaleExportSweepActivities(store, logger.NewNoopLogger())
	a.batchSize = 1
	n, err := a.SweepStaleExportRunsActivity(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, n)

	want := map[string]types.TaskStatus{
		"old_processing":    types.TaskStatusFailed,
		"old_pending":       types.TaskStatusFailed,
		"old_completed":     types.TaskStatusCompleted,
		"old_import":        types.TaskStatusProcessing,
		"recent_processing": types.TaskStatusProcessing,
	}
	for id, status := range want {
		got, err := store.Get(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, status, got.TaskStatus, id)
	}
	failed, err := store.Get(context.Background(), "old_pending")
	require.NoError(t, err)
	require.Equal(t, "Run did not finish.", *failed.ErrorSummary)
	require.NotNil(t, failed.FailedAt)
}
