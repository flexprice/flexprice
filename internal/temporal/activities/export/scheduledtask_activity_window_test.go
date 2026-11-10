package export

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/temporal"
)

type stubTaskRepo struct {
	scheduledtask.Repository
	row *scheduledtask.ScheduledTask
}

func (r *stubTaskRepo) Get(context.Context, string) (*scheduledtask.ScheduledTask, error) {
	return r.row, nil
}

type stubBoundary struct{}

func (stubBoundary) CalculateIntervalBoundaries(t time.Time, _ types.ScheduledTaskInterval) (time.Time, time.Time) {
	s := t.Truncate(24 * time.Hour)
	return s, s.Add(24 * time.Hour)
}

func newDetailsActivity(t *testing.T, row *scheduledtask.ScheduledTask) *ScheduledTaskActivity {
	t.Helper()
	log, err := logger.NewLogger(&config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}})
	require.NoError(t, err)
	return NewScheduledTaskActivity(&stubTaskRepo{row: row}, nil, log, stubBoundary{})
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339, s)
	require.NoError(t, err)
	return v
}

func TestDetails_ScheduledRowUsesScheduleWindow(t *testing.T) {
	a := newDetailsActivity(t, &scheduledtask.ScheduledTask{
		ID:          "st1",
		Interval:    types.ScheduledTaskIntervalDaily,
		Schedule:    &types.ExportSchedule{Minute: 30, Timezone: "Asia/Kolkata"},
		ManagedCopy: true,
	})
	got, err := a.GetScheduledTaskDetails(context.Background(), GetScheduledTaskDetailsInput{
		ScheduledTaskID:       "st1",
		WorkflowExecutionTime: mustTime(t, "2026-11-09T19:00:00Z"),
	})
	require.NoError(t, err)
	require.Equal(t, mustTime(t, "2026-11-08T18:30:00Z"), got.StartTime)
	require.Equal(t, mustTime(t, "2026-11-09T18:30:00Z"), got.EndTime)
	require.True(t, got.ManagedCopy)
}

func TestDetails_OldRowUnchanged(t *testing.T) {
	a := newDetailsActivity(t, &scheduledtask.ScheduledTask{ID: "st1", Interval: types.ScheduledTaskIntervalDaily})
	got, err := a.GetScheduledTaskDetails(context.Background(), GetScheduledTaskDetailsInput{
		ScheduledTaskID:       "st1",
		WorkflowExecutionTime: mustTime(t, "2026-11-09T19:00:00Z"),
	})
	require.NoError(t, err)
	require.Equal(t, mustTime(t, "2026-11-08T00:00:00Z"), got.StartTime)
	require.Equal(t, mustTime(t, "2026-11-09T00:00:00Z"), got.EndTime)
	require.False(t, got.ManagedCopy)
}

func TestDetails_ManualDefaultsToPreviousUTCDay(t *testing.T) {
	a := newDetailsActivity(t, &scheduledtask.ScheduledTask{ID: "st1", Interval: types.ScheduledTaskIntervalManual})
	got, err := a.GetScheduledTaskDetails(context.Background(), GetScheduledTaskDetailsInput{
		ScheduledTaskID:       "st1",
		WorkflowExecutionTime: mustTime(t, "2026-11-10T10:00:00Z"),
	})
	require.NoError(t, err)
	require.Equal(t, mustTime(t, "2026-11-09T00:00:00Z"), got.StartTime)
	require.Equal(t, mustTime(t, "2026-11-10T00:00:00Z"), got.EndTime)
}

func TestDetails_BadTimezoneIsNonRetryable(t *testing.T) {
	a := newDetailsActivity(t, &scheduledtask.ScheduledTask{
		ID:       "st1",
		Interval: types.ScheduledTaskIntervalDaily,
		Schedule: &types.ExportSchedule{Timezone: "Not/AZone"},
	})
	_, err := a.GetScheduledTaskDetails(context.Background(), GetScheduledTaskDetailsInput{
		ScheduledTaskID:       "st1",
		WorkflowExecutionTime: mustTime(t, "2026-11-09T19:00:00Z"),
	})
	var appErr *temporal.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.True(t, appErr.NonRetryable())
}
