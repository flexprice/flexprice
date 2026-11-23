package exportschedule

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"

	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	temporalClient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	exportWorkflows "github.com/flexprice/flexprice/internal/temporal/workflows/export"
	"github.com/flexprice/flexprice/internal/types"
)

type fakeHandle struct {
	models.ScheduleHandle
	err       error
	desc      *client.ScheduleDescription
	paused    int
	unpaused  int
	deleted   int
	updated   *client.Schedule
	describes int
}

func (h *fakeHandle) Pause(context.Context, client.SchedulePauseOptions) error {
	h.paused++
	return h.err
}

func (h *fakeHandle) Unpause(context.Context, client.ScheduleUnpauseOptions) error {
	h.unpaused++
	return nil
}

func (h *fakeHandle) Delete(context.Context) error {
	h.deleted++
	return h.err
}

func (h *fakeHandle) Describe(context.Context) (*client.ScheduleDescription, error) {
	h.describes++
	if h.err != nil {
		return nil, h.err
	}
	return h.desc, nil
}

func (h *fakeHandle) Update(_ context.Context, opts client.ScheduleUpdateOptions) error {
	if h.err != nil {
		return h.err
	}
	out, err := opts.DoUpdate(client.ScheduleUpdateInput{Description: client.ScheduleDescription{Schedule: client.Schedule{
		Spec:  &client.ScheduleSpec{},
		State: &client.ScheduleState{},
	}}})
	if err != nil {
		return err
	}
	h.updated = out.Schedule
	return nil
}

type fakeClient struct {
	temporalClient.TemporalClient
	handle    *fakeHandle
	ids       []string
	created   *models.CreateScheduleOptions
	createErr error
	started   *models.StartWorkflowOptions
	args      []interface{}
}

func (c *fakeClient) GetScheduleHandle(_ context.Context, id string) models.ScheduleHandle {
	c.ids = append(c.ids, id)
	return c.handle
}

func (c *fakeClient) CreateSchedule(_ context.Context, o models.CreateScheduleOptions) (models.ScheduleHandle, error) {
	c.created = &o
	return nil, c.createErr
}

type run struct{ models.WorkflowRun }

func (run) GetID() string    { return "wf_1" }
func (run) GetRunID() string { return "run_1" }

func (c *fakeClient) StartWorkflow(_ context.Context, o models.StartWorkflowOptions, _ interface{}, args ...interface{}) (models.WorkflowRun, error) {
	c.started = &o
	c.args = args
	return run{}, nil
}

func newTestScheduler(h *fakeHandle) (*fakeClient, Scheduler) {
	c := &fakeClient{handle: h}
	return c, NewScheduler(c, logger.NewNoopLogger())
}

func exportTask(interval types.ScheduledTaskInterval, schedule *types.ExportSchedule) *scheduledtask.ScheduledTask {
	return &scheduledtask.ScheduledTask{
		ID:                 "st_1",
		TenantID:           "t1",
		EnvironmentID:      "e1",
		Interval:           interval,
		Enabled:            true,
		Schedule:           schedule,
		TemporalScheduleID: "",
		CreatedBy:          "u1",
	}
}

func notFound() error { return serviceerror.NewNotFound("gone") }

func TestNewSchedulerNilClient(t *testing.T) {
	require.Nil(t, NewScheduler(nil, logger.NewNoopLogger()))
}

func TestSchedulerMissingScheduleIsSuccess(t *testing.T) {
	task := exportTask(types.ScheduledTaskIntervalDaily, nil)

	h := &fakeHandle{err: notFound()}
	c, s := newTestScheduler(h)
	require.NoError(t, s.Delete(context.Background(), task))
	require.NoError(t, s.Pause(context.Background(), task))
	synced, err := s.UpdateSpec(context.Background(), task)
	require.NoError(t, err)
	require.False(t, synced)
	require.Equal(t, []string{"st_1", "st_1", "st_1"}, c.ids)
}

func TestSchedulerOtherErrorsFail(t *testing.T) {
	task := exportTask(types.ScheduledTaskIntervalDaily, nil)
	h := &fakeHandle{err: errors.New("unavailable")}
	_, s := newTestScheduler(h)

	require.True(t, ierr.IsInternal(s.Delete(context.Background(), task)))
	require.True(t, ierr.IsInternal(s.Pause(context.Background(), task)))
	require.True(t, ierr.IsInternal(s.Resume(context.Background(), task)))
	_, err := s.UpdateSpec(context.Background(), task)
	require.True(t, ierr.IsInternal(err))
}

func TestSchedulerResumeCreatesMissingSchedule(t *testing.T) {
	task := exportTask(types.ScheduledTaskIntervalDaily, &types.ExportSchedule{Hour: 9, Minute: 30, Timezone: "Asia/Kolkata"})
	c, s := newTestScheduler(&fakeHandle{err: notFound()})

	require.NoError(t, s.Resume(context.Background(), task))
	require.NotNil(t, c.created)
	require.Equal(t, "st_1", c.created.ID)
	require.Equal(t, []string{"30 9 * * *"}, c.created.Spec.CronExpressions)
	require.Equal(t, "Asia/Kolkata", c.created.Spec.TimeZoneName)
	require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL, c.created.Overlap)
	require.Equal(t, CatchupWindow, c.created.CatchupWindow)
	require.False(t, c.created.Paused)
}

func TestSchedulerResumeUnpausesOnlyPaused(t *testing.T) {
	task := exportTask(types.ScheduledTaskIntervalDaily, nil)
	for _, paused := range []bool{true, false} {
		h := &fakeHandle{desc: &client.ScheduleDescription{Schedule: client.Schedule{State: &client.ScheduleState{Paused: paused}}}}
		c, s := newTestScheduler(h)
		require.NoError(t, s.Resume(context.Background(), task))
		require.Nil(t, c.created)
		want := 0
		if paused {
			want = 1
		}
		require.Equal(t, want, h.unpaused)
	}
}

func TestSchedulerManualHasNoSchedule(t *testing.T) {
	task := exportTask(types.ScheduledTaskIntervalManual, nil)
	c, s := newTestScheduler(&fakeHandle{})

	require.NoError(t, s.Create(context.Background(), task))
	require.NoError(t, s.Resume(context.Background(), task))
	require.NoError(t, s.Pause(context.Background(), task))
	require.NoError(t, s.Delete(context.Background(), task))
	synced, err := s.UpdateSpec(context.Background(), task)
	require.NoError(t, err)
	require.False(t, synced)
	require.Nil(t, c.created)
	require.Empty(t, c.ids)
}

func TestSchedulerCreateLegacyRow(t *testing.T) {
	c, s := newTestScheduler(&fakeHandle{})
	require.NoError(t, s.Create(context.Background(), exportTask(types.ScheduledTaskIntervalHourly, nil)))

	require.Equal(t, []string{"15 * * * *"}, c.created.Spec.CronExpressions)
	require.Empty(t, c.created.Spec.TimeZoneName)
	require.Zero(t, c.created.Overlap)
	require.Zero(t, c.created.CatchupWindow)
	require.Equal(t, string(types.TemporalTaskQueueExport), c.created.Action.TaskQueue)
	require.Equal(t, workflowTimeout, c.created.Action.WorkflowExecutionTimeout)
	input := c.created.Action.Args[0].(exportWorkflows.ExecuteExportWorkflowInput)
	require.Equal(t, "u1", input.UserID)
	require.Equal(t, "st_1", input.ScheduledTaskID)
}

func TestSchedulerUpdateSpecSetsTimezonePolicy(t *testing.T) {
	h := &fakeHandle{}
	_, s := newTestScheduler(h)
	synced, err := s.UpdateSpec(context.Background(), exportTask(types.ScheduledTaskIntervalDaily, &types.ExportSchedule{Hour: 2, Timezone: "UTC"}))
	require.NoError(t, err)
	require.True(t, synced)
	require.Equal(t, []string{"0 2 * * *"}, h.updated.Spec.CronExpressions)
	require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL, h.updated.Policy.Overlap)
	require.Equal(t, CatchupWindow, h.updated.Policy.CatchupWindow)
}

func TestSchedulerStartRun(t *testing.T) {
	c, s := newTestScheduler(&fakeHandle{})
	id, err := s.StartRun(context.Background(), exportTask(types.ScheduledTaskIntervalDaily, nil), Run{WorkflowID: "task_1", UserID: "u2", CustomerExternalIDs: []string{"acme"}})
	require.NoError(t, err)
	require.Equal(t, "wf_1", id)
	require.Equal(t, "task_1", c.started.ID)
	require.Equal(t, string(types.TemporalTaskQueueExport), c.started.TaskQueue)
	input := c.args[0].(exportWorkflows.ExecuteExportWorkflowInput)
	require.Equal(t, "u2", input.UserID)
	require.Equal(t, []string{"acme"}, input.CustomerExternalIDs)
	require.Nil(t, input.CustomStartTime)
}
