//go:build pgintegration

package ent

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// Needs the pgintegration build tag.
func TestScheduledTaskRepository_NewColumns(t *testing.T) {
	cfg, err := config.NewConfig()
	require.NoError(t, err)
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	clients, err := postgres.NewEntClients(cfg, log)
	require.NoError(t, err)
	repo := NewScheduledTaskRepository(postgres.NewClient(clients, log, nil), log)

	runID := types.GenerateUUID()
	ctxFor := func(env string) context.Context {
		return types.SetUserID(types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), env), "user_test")
	}
	ctx := ctxFor("env_" + runID)
	otherCtx := ctxFor("env_other_" + runID)

	schedule := &types.ExportSchedule{Hour: 9, Minute: 0, Timezone: "Asia/Kolkata"}
	newRow := &scheduledtask.ScheduledTask{
		ID:                 "st_" + runID + "_new",
		ConnectionID:       "",
		EntityType:         types.ScheduledTaskEntityTypeEvents,
		Interval:           types.ScheduledTaskIntervalDaily,
		Enabled:            true,
		Name:               "Daily usage",
		ManagedCopy:        true,
		Schedule:           schedule,
		TemporalScheduleID: "sched_" + runID,
		Status:             types.StatusPublished,
		CreatedBy:          "user_test",
		UpdatedBy:          "user_test",
	}
	require.NoError(t, repo.Create(ctx, newRow))

	oldRow := &scheduledtask.ScheduledTask{
		ID:           "st_" + runID + "_old",
		ConnectionID: "conn_" + runID,
		EntityType:   types.ScheduledTaskEntityTypeEvents,
		Interval:     types.ScheduledTaskIntervalDaily,
		Enabled:      true,
		Status:       types.StatusPublished,
		CreatedBy:    "user_test",
		UpdatedBy:    "user_test",
	}
	require.NoError(t, repo.Create(ctx, oldRow))

	otherEnvRow := &scheduledtask.ScheduledTask{
		ID:          "st_" + runID + "_other",
		EntityType:  types.ScheduledTaskEntityTypeEvents,
		Interval:    types.ScheduledTaskIntervalDaily,
		Enabled:     true,
		ManagedCopy: true,
		Status:      types.StatusPublished,
	}
	require.NoError(t, repo.Create(otherCtx, otherEnvRow))

	got, err := repo.Get(ctx, newRow.ID)
	require.NoError(t, err)
	require.Equal(t, "", got.ConnectionID)
	require.True(t, got.ManagedCopy)
	require.Equal(t, "Daily usage", got.Name)
	require.Equal(t, schedule, got.Schedule)

	gotOld, err := repo.Get(ctx, oldRow.ID)
	require.NoError(t, err)
	require.Equal(t, "", gotOld.Name)
	require.False(t, gotOld.ManagedCopy)
	require.Nil(t, gotOld.Schedule)
	require.Nil(t, gotOld.SourceConfig)

	srcCfg := &types.ExportSourceConfig{ReportKey: "invoice_total", ReportParams: &types.ReportParams{Shape: "timeseries", Dimensions: []string{"customer"}}}
	reportRow := &scheduledtask.ScheduledTask{
		ID:           "st_" + runID + "_report",
		EntityType:   types.ScheduledTaskEntityTypeReport,
		Interval:     types.ScheduledTaskIntervalManual,
		Enabled:      true,
		ManagedCopy:  true,
		SourceConfig: srcCfg,
		Status:       types.StatusPublished,
	}
	require.NoError(t, repo.Create(ctx, reportRow))
	gotReport, err := repo.Get(ctx, reportRow.ID)
	require.NoError(t, err)
	require.Equal(t, srcCfg, gotReport.SourceConfig)

	rows, err := repo.List(ctx, &scheduledtask.ListFilters{})
	require.NoError(t, err)
	ids := lo.Map(rows, func(r *scheduledtask.ScheduledTask, _ int) string { return r.ID })
	require.ElementsMatch(t, []string{newRow.ID, oldRow.ID, reportRow.ID}, ids)

	got.Name = "Renamed"
	got.Enabled = false
	got.Schedule = nil
	got.TemporalScheduleID = ""
	got.UpdatedBy = "user_other"
	require.NoError(t, repo.UpdateSettings(ctx, got))

	updated, err := repo.Get(ctx, newRow.ID)
	require.NoError(t, err)
	require.Equal(t, "Renamed", updated.Name)
	require.False(t, updated.Enabled)
	require.Nil(t, updated.Schedule)
	require.Equal(t, "", updated.TemporalScheduleID)
	require.Equal(t, "user_other", updated.UpdatedBy)
	require.True(t, updated.UpdatedAt.Equal(got.UpdatedAt))
	require.True(t, updated.ManagedCopy)

	otherEnvRow.Name = "hijack"
	err = repo.UpdateSettings(ctx, otherEnvRow)
	require.True(t, ierr.IsNotFound(err))
	untouched, err := repo.Get(otherCtx, otherEnvRow.ID)
	require.NoError(t, err)
	require.Equal(t, "", untouched.Name)
}
