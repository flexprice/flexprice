package cron

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
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

	s3 := &task.Task{ID: "old_s3", TaskType: types.TaskTypeExport, TaskStatus: types.TaskStatusProcessing, EnvironmentID: "e1", ScheduledTaskID: "st_s3"}
	s3.TenantID = "t1"
	s3.Status = types.StatusPublished
	s3.CreatedAt = clock.Now().Add(-3 * time.Hour)
	require.NoError(t, store.Create(context.Background(), s3))

	exports := fakeScheduledTasks{rows: map[string]*scheduledtask.ScheduledTask{"st_s3": {ID: "st_s3", ConnectionID: "conn_1", JobConfig: &types.S3JobConfig{Bucket: "b", KeyPrefix: "p/"}}}}
	a := NewStaleExportSweepActivities(store, exports, logger.NewNoopLogger())
	a.batchSize = 1
	n, err := a.SweepStaleExportRunsActivity(context.Background())
	require.NoError(t, err)
	require.Equal(t, 4, n)

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
	require.Equal(t, "The run stopped before finishing. Run it again.", *failed.ErrorSummary)
	require.NotNil(t, failed.FailedAt)
	require.Equal(t, types.DefaultUserID, failed.UpdatedBy)
	ds := failed.Metadata["deliveries"].([]types.ExportDelivery)
	require.Len(t, ds, 1)
	require.Equal(t, types.ExportDeliveryTypeFlexprice, ds[0].Type)
	require.Equal(t, types.ExportDeliveryStatusFailed, ds[0].Status)
	require.Equal(t, "The run stopped before finishing. Run it again.", ds[0].Detail)

	viaS3, err := store.Get(context.Background(), "old_s3")
	require.NoError(t, err)
	ds = viaS3.Metadata["deliveries"].([]types.ExportDelivery)
	require.Equal(t, types.ExportDeliveryTypeS3, ds[0].Type)
	require.Equal(t, "b/p/", ds[0].Target)
	require.Equal(t, types.ExportDeliveryStatusFailed, ds[0].Status)

	done, err := store.Get(context.Background(), "old_completed")
	require.NoError(t, err)
	require.NotContains(t, done.Metadata, "deliveries")
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

	a := NewStaleExportSweepActivities(flakyFailStore{store, "a_bad"}, fakeScheduledTasks{}, logger.NewNoopLogger())
	n, err := a.SweepStaleExportRunsActivity(context.Background())
	require.Error(t, err)
	require.Equal(t, 1, n)

	good, err := store.Get(context.Background(), "b_ok")
	require.NoError(t, err)
	require.Equal(t, types.TaskStatusFailed, good.TaskStatus)
}

type fakeScheduledTasks struct {
	scheduledtask.Repository
	rows map[string]*scheduledtask.ScheduledTask
}

func (f fakeScheduledTasks) Get(_ context.Context, id string) (*scheduledtask.ScheduledTask, error) {
	if st, ok := f.rows[id]; ok {
		return st, nil
	}
	return nil, errors.New("not found")
}
