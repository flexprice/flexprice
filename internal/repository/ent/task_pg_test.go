//go:build pgintegration

package ent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent/task"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/config"
	domainTask "github.com/flexprice/flexprice/internal/domain/task"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// Needs the pgintegration build tag.
func TestTaskRepository_StaleExportTasks(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	pgClient := postgres.NewClient(clients, log, nil)
	repo := NewTaskRepository(pgClient, log)

	runID := types.GenerateUUID()
	ctxFor := func(tenant, env string) context.Context {
		return types.SetUserID(types.SetEnvironmentID(types.SetTenantID(context.Background(), tenant), env), "user_test")
	}
	tenantA := ctxFor(types.DefaultTenantID, "env_a_"+runID)
	tenantB := ctxFor("tenant_b_"+runID, "env_b_"+runID)
	prefix := "task_" + runID + "_"

	var created []string
	t.Cleanup(func() {
		_, err := pgClient.Writer(context.Background()).Task.Delete().
			Where(task.IDIn(created...)).Exec(context.Background())
		require.NoError(t, err)
	})

	create := func(ctx context.Context, suffix string, tt types.TaskType, status types.TaskStatus, age time.Duration) string {
		row := &domainTask.Task{
			ID:         prefix + suffix,
			TaskType:   tt,
			EntityType: types.EntityTypeEvents,
			FileType:   types.FileTypeCSV,
			TaskStatus: status,
			Metadata:   map[string]interface{}{},
			BaseModel: types.BaseModel{
				TenantID:  types.GetTenantID(ctx),
				Status:    types.StatusPublished,
				CreatedAt: clock.Now().Add(-age),
				UpdatedAt: clock.Now().Add(-age),
				CreatedBy: "user_test",
				UpdatedBy: "user_test",
			},
			EnvironmentID: types.GetEnvironmentID(ctx),
		}
		created = append(created, row.ID)
		require.NoError(t, repo.Create(ctx, row))
		return row.ID
	}

	staleA := create(tenantA, "a_processing", types.TaskTypeExport, types.TaskStatusProcessing, 3*time.Hour)
	staleB := create(tenantB, "b_pending", types.TaskTypeExport, types.TaskStatusPending, 3*time.Hour)
	done := create(tenantA, "a_completed", types.TaskTypeExport, types.TaskStatusCompleted, 3*time.Hour)
	imp := create(tenantA, "a_import", types.TaskTypeImport, types.TaskStatusProcessing, 3*time.Hour)
	recent := create(tenantA, "a_recent", types.TaskTypeExport, types.TaskStatusProcessing, time.Hour)

	cutoff := clock.Now().Add(-2 * time.Hour)
	refs, err := repo.ListStaleExportTasks(context.Background(), cutoff, 100000)
	require.NoError(t, err)
	got := map[string]domainTask.StaleRef{}
	for _, ref := range refs {
		if strings.HasPrefix(ref.ID, prefix) {
			got[ref.ID] = ref
		}
	}
	require.Len(t, got, 2)
	require.Equal(t, "tenant_b_"+runID, got[staleB].TenantID)
	require.Equal(t, "env_b_"+runID, got[staleB].EnvironmentID)
	require.Equal(t, types.DefaultTenantID, got[staleA].TenantID)

	changed, err := repo.FailStaleExportTask(tenantB, staleA, cutoff, "Run did not finish.")
	require.NoError(t, err)
	require.False(t, changed)
	row, err := repo.Get(tenantA, staleA)
	require.NoError(t, err)
	require.Equal(t, types.TaskStatusProcessing, row.TaskStatus)

	for id, ctx := range map[string]context.Context{staleA: tenantA, staleB: tenantB} {
		changed, err := repo.FailStaleExportTask(ctx, id, cutoff, "Run did not finish.")
		require.NoError(t, err)
		require.True(t, changed)
		row, err := repo.Get(ctx, id)
		require.NoError(t, err)
		require.Equal(t, types.TaskStatusFailed, row.TaskStatus)
		require.Equal(t, "Run did not finish.", *row.ErrorSummary)
		require.NotNil(t, row.FailedAt)
	}

	changed, err = repo.FailStaleExportTask(tenantA, staleA, cutoff, "again")
	require.NoError(t, err)
	require.False(t, changed)

	for id, want := range map[string]types.TaskStatus{
		done: types.TaskStatusCompleted, imp: types.TaskStatusProcessing, recent: types.TaskStatusProcessing,
	} {
		changed, err := repo.FailStaleExportTask(tenantA, id, cutoff, "x")
		require.NoError(t, err)
		require.False(t, changed)
		row, err := repo.Get(tenantA, id)
		require.NoError(t, err)
		require.Equal(t, want, row.TaskStatus)
	}

	// Deliveries write touches only metadata.
	require.NoError(t, repo.SetTaskMetadataKey(tenantA, done, "deliveries", []map[string]string{{"type": "flexprice", "status": "delivered"}}))
	row, err = repo.Get(tenantA, done)
	require.NoError(t, err)
	require.Equal(t, types.TaskStatusCompleted, row.TaskStatus)
	require.Len(t, row.Metadata["deliveries"], 1)
	require.Error(t, repo.SetTaskMetadataKey(tenantB, done, "deliveries", []string{}))

	// Guarded update never reopens a completed run.
	row.TaskStatus = types.TaskStatusFailed
	changedRow, err := repo.UpdateUnlessCompleted(tenantA, row)
	require.NoError(t, err)
	require.False(t, changedRow)
	row, err = repo.Get(tenantA, done)
	require.NoError(t, err)
	require.Equal(t, types.TaskStatusCompleted, row.TaskStatus)
	require.Len(t, row.Metadata["deliveries"], 1)
}
