package cron

import (
	"context"
	"errors"
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

	other := &task.Task{ID: "other_tenant", TaskType: types.TaskTypeExport, TaskStatus: types.TaskStatusPending, EnvironmentID: "e2"}
	other.TenantID = "t2"
	other.Status = types.StatusPublished
	other.CreatedAt = clock.Now().Add(-3 * time.Hour)
	require.NoError(t, store.Create(context.Background(), other))

	a := NewStaleExportSweepActivities(store, logger.NewNoopLogger())
	a.batchSize = 1
	n, err := a.SweepStaleExportRunsActivity(context.Background())
	require.NoError(t, err)
	require.Equal(t, 3, n)

	want := map[string]types.TaskStatus{
		"other_tenant":      types.TaskStatusFailed,
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
	require.Equal(t, types.DefaultUserID, failed.UpdatedBy)
}

type flakyFailStore struct {
	*testutil.InMemoryTaskStore
	badID string
}

func (s flakyFailStore) FailStaleExportTask(ctx context.Context, id string, olderThan time.Time, reason string) (bool, error) {
	if id == s.badID {
		return false, errors.New("boom")
	}
	return s.InMemoryTaskStore.FailStaleExportTask(ctx, id, olderThan, reason)
}

func TestSweepStaleExportRuns_ReturnsErrorAfterAttemptingAllRows(t *testing.T) {
	store := testutil.NewInMemoryTaskStore()
	seedTask(t, store, "a_bad", types.TaskTypeExport, types.TaskStatusProcessing, 4*time.Hour)
	seedTask(t, store, "b_ok", types.TaskTypeExport, types.TaskStatusProcessing, 3*time.Hour)

	a := NewStaleExportSweepActivities(flakyFailStore{store, "a_bad"}, logger.NewNoopLogger())
	n, err := a.SweepStaleExportRunsActivity(context.Background())
	require.Error(t, err)
	require.Equal(t, 1, n)

	good, err := store.Get(context.Background(), "b_ok")
	require.NoError(t, err)
	require.Equal(t, types.TaskStatusFailed, good.TaskStatus)
}
