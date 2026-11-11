package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
	sdkclient "go.temporal.io/sdk/client"
)

type fakeHandle struct {
	models.ScheduleHandle
	schedule    sdkclient.Schedule
	describeErr error
	updateErr   error
	updates     int
}

func (h *fakeHandle) Describe(context.Context) (*sdkclient.ScheduleDescription, error) {
	if h.describeErr != nil {
		return nil, h.describeErr
	}
	return &sdkclient.ScheduleDescription{Schedule: h.schedule}, nil
}

func (h *fakeHandle) Update(_ context.Context, opts sdkclient.ScheduleUpdateOptions) error {
	if h.updateErr != nil {
		return h.updateErr
	}
	out, err := opts.DoUpdate(sdkclient.ScheduleUpdateInput{Description: sdkclient.ScheduleDescription{Schedule: h.schedule}})
	if err != nil || out == nil {
		return err
	}
	h.updates++
	h.schedule = *out.Schedule
	return nil
}

type fakeHandles map[string]*fakeHandle

func (f fakeHandles) GetScheduleHandle(_ context.Context, id string) models.ScheduleHandle {
	return f[id]
}

type fakeTenants struct{ tenant.Repository }

func (fakeTenants) List(context.Context) ([]*tenant.Tenant, error) {
	return []*tenant.Tenant{{ID: "t1"}}, nil
}

type fakeEnvs struct{ environment.Repository }

func (fakeEnvs) List(ctx context.Context, _ types.Filter) ([]*environment.Environment, error) {
	if types.GetTenantID(ctx) != "t1" {
		return nil, errors.New("tenant not scoped")
	}
	return []*environment.Environment{{ID: "e1"}}, nil
}

type fakeScheduledTasks struct {
	scheduledtask.Repository
	rows []*scheduledtask.ScheduledTask
}

func (f fakeScheduledTasks) List(ctx context.Context, _ *scheduledtask.ListFilters) ([]*scheduledtask.ScheduledTask, error) {
	if types.GetTenantID(ctx) != "t1" || types.GetEnvironmentID(ctx) != "e1" {
		return nil, errors.New("not scoped")
	}
	return f.rows, nil
}

func scheduleWithTimeout(d time.Duration) sdkclient.Schedule {
	return sdkclient.Schedule{Action: &sdkclient.ScheduleWorkflowAction{
		Args:                     []interface{}{"keep"},
		WorkflowExecutionTimeout: d,
		WorkflowRunTimeout:       d,
	}}
}

func TestReconcileExportScheduleTimeouts(t *testing.T) {
	old := &fakeHandle{schedule: scheduleWithTimeout(60 * time.Minute)}
	current := &fakeHandle{schedule: scheduleWithTimeout(types.ExportWorkflowTimeout)}
	missing := &fakeHandle{describeErr: serviceerror.NewNotFound("gone")}
	broken := &fakeHandle{schedule: scheduleWithTimeout(60 * time.Minute), updateErr: errors.New("boom")}
	handles := fakeHandles{"old": old, "current": current, "missing": missing, "broken": broken}

	rows := []*scheduledtask.ScheduledTask{
		{ID: "a", TemporalScheduleID: "old", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "b", TemporalScheduleID: "current", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "c", TemporalScheduleID: "missing", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "d", TemporalScheduleID: "broken", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "e", TemporalScheduleID: "", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "f", TemporalScheduleID: "old", Interval: types.ScheduledTaskIntervalManual},
	}

	run := func() {
		ReconcileExportScheduleTimeouts(context.Background(), handles, fakeTenants{}, fakeEnvs{}, fakeScheduledTasks{rows: rows}, logger.NewNoopLogger())
	}
	run()

	action := old.schedule.Action.(*sdkclient.ScheduleWorkflowAction)
	require.Equal(t, types.ExportWorkflowTimeout, action.WorkflowExecutionTimeout)
	require.Equal(t, types.ExportWorkflowTimeout, action.WorkflowRunTimeout)
	require.Equal(t, []interface{}{"keep"}, action.Args)
	require.Equal(t, 1, old.updates)
	require.Equal(t, 0, current.updates)

	run()
	require.Equal(t, 1, old.updates)
}
