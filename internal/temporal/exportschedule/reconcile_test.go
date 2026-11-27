package exportschedule

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/cache"
	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/domain/scheduledtask"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	sdkclient "go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

type rcHandle struct {
	models.ScheduleHandle
	schedule    sdkclient.Schedule
	describeErr error
	updateErr   error
	updates     int
	onDescribe  func()
}

func (h *rcHandle) Describe(context.Context) (*sdkclient.ScheduleDescription, error) {
	if h.onDescribe != nil {
		h.onDescribe()
	}
	if h.describeErr != nil {
		return nil, h.describeErr
	}
	return &sdkclient.ScheduleDescription{Schedule: h.schedule}, nil
}

func (h *rcHandle) Update(_ context.Context, opts sdkclient.ScheduleUpdateOptions) error {
	if h.updateErr != nil {
		return h.updateErr
	}
	out, err := opts.DoUpdate(sdkclient.ScheduleUpdateInput{Description: sdkclient.ScheduleDescription{Schedule: h.schedule}})
	if errors.Is(err, temporal.ErrSkipScheduleUpdate) {
		return nil
	}
	if err != nil {
		return err
	}
	h.updates++
	h.schedule = *out.Schedule
	return nil
}

type rcHandles map[string]*rcHandle

func (f rcHandles) GetScheduleHandle(_ context.Context, id string) models.ScheduleHandle {
	if h, ok := f[id]; ok {
		return h
	}
	return &rcHandle{describeErr: serviceerror.NewNotFound("gone")}
}

type fakeTenants struct{ tenant.Repository }

func (fakeTenants) ListIDs(context.Context) ([]string, error) {
	return []string{"t1"}, nil
}

type flakyTenants struct {
	tenant.Repository
	failures int
	calls    *int
}

func (f flakyTenants) ListIDs(context.Context) ([]string, error) {
	*f.calls++
	if *f.calls <= f.failures {
		return nil, errors.New("db down")
	}
	return []string{"t1"}, nil
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
	old := &rcHandle{schedule: scheduleWithTimeout(60 * time.Minute)}
	current := &rcHandle{schedule: scheduleWithTimeout(types.ExportWorkflowTimeout)}
	missing := &rcHandle{describeErr: serviceerror.NewNotFound("gone")}
	broken := &rcHandle{schedule: scheduleWithTimeout(60 * time.Minute), updateErr: errors.New("boom")}
	handles := rcHandles{"old": old, "current": current, "missing": missing, "broken": broken}

	rows := []*scheduledtask.ScheduledTask{
		{ID: "old", TemporalScheduleID: "old", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "current", TemporalScheduleID: "current", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "missing", TemporalScheduleID: "missing", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "broken", TemporalScheduleID: "broken", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "e", TemporalScheduleID: "", Interval: types.ScheduledTaskIntervalDaily},
		{ID: "old", TemporalScheduleID: "old", Interval: types.ScheduledTaskIntervalManual},
	}

	run := func() bool {
		return reconcileExportScheduleTimeouts(context.Background(), handles, fakeTenants{}, fakeEnvs{}, fakeScheduledTasks{rows: rows}, logger.NewNoopLogger(), 10, 10, time.Millisecond)
	}
	require.False(t, run(), "a failed schedule fails the run")

	action := old.schedule.Action.(*sdkclient.ScheduleWorkflowAction)
	require.Equal(t, types.ExportWorkflowTimeout, action.WorkflowExecutionTimeout)
	require.Equal(t, types.ExportWorkflowTimeout, action.WorkflowRunTimeout)
	require.Equal(t, []interface{}{"keep"}, action.Args)
	require.Equal(t, 1, old.updates)
	require.Equal(t, 0, current.updates)

	run()
	require.Equal(t, 1, old.updates)
}

type racedHandle struct {
	rcHandle
	described sdkclient.Schedule
}

func (h *racedHandle) Describe(context.Context) (*sdkclient.ScheduleDescription, error) {
	return &sdkclient.ScheduleDescription{Schedule: h.described}, nil
}

type racedGetter struct{ h *racedHandle }

func (g racedGetter) GetScheduleHandle(context.Context, string) models.ScheduleHandle { return g.h }

func TestReconcileExportSchedule_AlreadyCurrentSkips(t *testing.T) {
	h := &racedHandle{
		rcHandle:  rcHandle{schedule: scheduleWithTimeout(types.ExportWorkflowTimeout)},
		described: scheduleWithTimeout(60 * time.Minute),
	}

	changed, _, err := reconcileExportSchedule(context.Background(), racedGetter{h}, &scheduledtask.ScheduledTask{ID: "s", Interval: types.ScheduledTaskIntervalDaily})
	require.NoError(t, err)
	require.False(t, changed)
	require.Zero(t, h.updates)
}

type pagedEnvs struct {
	environment.Repository
	ids []string
}

func (p pagedEnvs) List(_ context.Context, f types.Filter) ([]*environment.Environment, error) {
	var out []*environment.Environment
	for i := f.Offset; i < len(p.ids) && len(out) < f.Limit; i++ {
		out = append(out, &environment.Environment{ID: p.ids[i]})
	}
	return out, nil
}

type keysetTasks struct {
	scheduledtask.Repository
	byEnv map[string][]*scheduledtask.ScheduledTask
}

func (k keysetTasks) List(ctx context.Context, f *scheduledtask.ListFilters) ([]*scheduledtask.ScheduledTask, error) {
	if !f.KeysetByID {
		return nil, errors.New("keyset required")
	}
	var out []*scheduledtask.ScheduledTask
	for _, r := range k.byEnv[types.GetEnvironmentID(ctx)] {
		if r.ID > f.AfterID && len(out) < f.Limit {
			out = append(out, r)
		}
	}
	return out, nil
}

func TestReconcileExportScheduleTimeouts_Paging(t *testing.T) {
	handles := rcHandles{}
	byEnv := map[string][]*scheduledtask.ScheduledTask{}
	for _, env := range []string{"e1", "e2", "e3"} {
		for _, n := range []string{"1", "2", "3"} {
			id := env + "-" + n
			handles[id] = &rcHandle{schedule: scheduleWithTimeout(60 * time.Minute)}
			byEnv[env] = append(byEnv[env], &scheduledtask.ScheduledTask{ID: id, TemporalScheduleID: id, Interval: types.ScheduledTaskIntervalDaily})
		}
	}

	reconcileExportScheduleTimeouts(context.Background(), handles, fakeTenants{}, pagedEnvs{ids: []string{"e1", "e2", "e3"}}, keysetTasks{byEnv: byEnv}, logger.NewNoopLogger(), 2, 2, time.Millisecond)

	for id, h := range handles {
		require.Equal(t, 1, h.updates, id)
	}
}

func TestReconcileExportScheduleTimeouts_RetriesEarlyFailure(t *testing.T) {
	tests := []struct {
		name      string
		failures  int
		wantCalls int
		wantFixed bool
	}{
		{"recovers on retry", 2, 3, true},
		{"gives up after three", 5, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &rcHandle{schedule: scheduleWithTimeout(60 * time.Minute)}
			rows := []*scheduledtask.ScheduledTask{{ID: "s", TemporalScheduleID: "s", Interval: types.ScheduledTaskIntervalDaily}}
			calls := 0
			reconcileExportScheduleTimeouts(context.Background(), rcHandles{"s": h}, flakyTenants{failures: tt.failures, calls: &calls},
				fakeEnvs{}, fakeScheduledTasks{rows: rows}, logger.NewNoopLogger(), 10, 10, time.Millisecond)
			require.Equal(t, tt.wantCalls, calls)
			require.Equal(t, tt.wantFixed, h.updates == 1)
		})
	}
}

type fakeLocker struct{ held map[string]bool }

type fakeLock struct {
	l   *fakeLocker
	key string
	ok  bool
}

func (f *fakeLocker) AcquireLock(_ context.Context, key string, _ time.Duration) (cache.Lock, error) {
	if f.held[key] {
		return &fakeLock{l: f, key: key}, nil
	}
	f.held[key] = true
	return &fakeLock{l: f, key: key, ok: true}, nil
}

func (l *fakeLock) AcquiredSuccessfully() bool { return l.ok }

func (l *fakeLock) Release(context.Context) error {
	delete(l.l.held, l.key)
	return nil
}

func TestReconcileExportScheduleTimeouts_RunsOncePerMarker(t *testing.T) {
	locker := &fakeLocker{held: map[string]bool{}}
	rows := []*scheduledtask.ScheduledTask{{ID: "s", TemporalScheduleID: "s", Interval: types.ScheduledTaskIntervalDaily}}
	calls := 0
	tenants := flakyTenants{calls: &calls}
	run := func(ctx context.Context, tenants tenant.Repository) {
		h := &rcHandle{schedule: scheduleWithTimeout(60 * time.Minute)}
		Reconcile(ctx, rcHandles{"s": h}, locker, tenants, fakeEnvs{}, fakeScheduledTasks{rows: rows}, logger.NewNoopLogger())
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	run(cancelled, flakyTenants{failures: 5, calls: &calls})
	require.Equal(t, 1, calls)
	require.Empty(t, locker.held, "failed run releases the marker")

	run(context.Background(), tenants)
	require.Equal(t, 2, calls)
	require.True(t, locker.held[reconcileMarkerKey()])

	run(context.Background(), tenants)
	require.Equal(t, 2, calls, "second pod skips")
}

func TestReconcilePass_ErrorsOnFailureOrCancel(t *testing.T) {
	rows := []*scheduledtask.ScheduledTask{{ID: "s", TemporalScheduleID: "s", Interval: types.ScheduledTaskIntervalDaily}}
	pass := func(ctx context.Context, h *rcHandle) error {
		return reconcilePass(ctx, rcHandles{"s": h}, fakeTenants{}, fakeEnvs{}, fakeScheduledTasks{rows: rows}, logger.NewNoopLogger(), 10, 10)
	}

	require.NoError(t, pass(context.Background(), &rcHandle{schedule: scheduleWithTimeout(60 * time.Minute)}))
	require.Error(t, pass(context.Background(), &rcHandle{describeErr: errors.New("unavailable")}))

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	require.ErrorIs(t, pass(cancelled, &rcHandle{schedule: scheduleWithTimeout(types.ExportWorkflowTimeout)}), context.Canceled)
}

func TestReconcileExportScheduleTimeouts_ScheduleFailureReleasesMarker(t *testing.T) {
	locker := &fakeLocker{held: map[string]bool{}}
	rows := []*scheduledtask.ScheduledTask{{ID: "s", TemporalScheduleID: "s", Interval: types.ScheduledTaskIntervalDaily}}
	ctx, cancel := context.WithCancel(context.Background())
	broken := &rcHandle{describeErr: errors.New("unavailable")}
	broken.onDescribe = cancel
	Reconcile(ctx, rcHandles{"s": broken}, locker, fakeTenants{}, fakeEnvs{}, fakeScheduledTasks{rows: rows}, logger.NewNoopLogger())
	require.Empty(t, locker.held)
}

func TestReconcileExportSchedule_DriftIsLoggedNotFixed(t *testing.T) {
	paused := func(p bool) sdkclient.Schedule {
		s := scheduleWithTimeout(types.ExportWorkflowTimeout)
		s.State = &sdkclient.ScheduleState{Paused: p}
		return s
	}
	tests := []struct {
		name      string
		enabled   bool
		handle    *rcHandle
		wantDrift string
	}{
		{"enabled missing", true, &rcHandle{describeErr: serviceerror.NewNotFound("gone")}, driftMissing},
		{"disabled missing", false, &rcHandle{describeErr: serviceerror.NewNotFound("gone")}, ""},
		{"enabled paused", true, &rcHandle{schedule: paused(true)}, driftPaused},
		{"disabled unpaused", false, &rcHandle{schedule: paused(false)}, driftUnpaused},
		{"enabled running", true, &rcHandle{schedule: paused(false)}, ""},
		{"disabled paused", false, &rcHandle{schedule: paused(true)}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &scheduledtask.ScheduledTask{ID: "s", Interval: types.ScheduledTaskIntervalDaily, Enabled: tt.enabled, Status: types.StatusPublished}
			changed, drift, err := reconcileExportSchedule(context.Background(), rcHandles{"s": tt.handle}, task)
			require.NoError(t, err)
			require.False(t, changed)
			require.Equal(t, tt.wantDrift, drift)
			require.Zero(t, tt.handle.updates)
		})
	}
}

func TestReconcileExportSchedule_AlignsTimezonePolicyAndKeepsPause(t *testing.T) {
	s := scheduleWithTimeout(types.ExportWorkflowTimeout)
	s.State = &sdkclient.ScheduleState{Paused: true}
	h := &rcHandle{schedule: s}
	task := &scheduledtask.ScheduledTask{ID: "s", Interval: types.ScheduledTaskIntervalDaily, Enabled: true, Status: types.StatusPublished,
		Schedule: &types.ExportSchedule{Hour: 2, Timezone: "Asia/Kolkata"}}

	changed, drift, err := reconcileExportSchedule(context.Background(), rcHandles{"s": h}, task)
	require.NoError(t, err)
	require.True(t, changed)
	require.Equal(t, driftPaused, drift)
	require.Equal(t, enums.SCHEDULE_OVERLAP_POLICY_BUFFER_ALL, h.schedule.Policy.Overlap)
	require.Equal(t, CatchupWindow, h.schedule.Policy.CatchupWindow)
	require.True(t, h.schedule.State.Paused)

	changed, _, err = reconcileExportSchedule(context.Background(), rcHandles{"s": h}, task)
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, 1, h.updates)
}

func TestReconcileMarkerKeyHasBuilderVersion(t *testing.T) {
	require.Contains(t, reconcileMarkerKey(), fmt.Sprintf(":v%d:", builderVersion))
}
