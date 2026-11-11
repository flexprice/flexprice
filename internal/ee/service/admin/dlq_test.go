package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/kafka/dlq"
	"github.com/flexprice/flexprice/internal/logger"
	dlqmodels "github.com/flexprice/flexprice/internal/temporal/models/dlq"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
)

func TestDLQReplayValidation(t *testing.T) {
	for _, req := range []admindto.ReplayDLQRequest{
		{}, {SourceTopic: " "}, {SourceTopic: "events_dlq", Max: -1},
		{SourceTopic: "events_dlq", MaxReplays: -1},
		{SourceTopic: "events_dlq", Since: "invalid"},
		{SourceTopic: "events_dlq", Since: "1970-01-01T00:00:00Z"},
		{SourceTopic: "events_dlq", Since: "2026-10-01T00:00:00Z", FromStart: true},
	} {
		svc := &dlqService{replay: func(context.Context, *config.Configuration, *logger.Logger, dlq.Options) (*dlq.Summary, error) {
			t.Fatal("invalid request reached Kafka")
			return nil, nil
		}, runs: &fakeRuns{t: t, failOnStart: true}}
		_, err := svc.Preview(context.Background(), req)
		require.Error(t, err)
		_, err = svc.StartReplay(context.Background(), req)
		require.Error(t, err)
	}
}

func TestDLQReplayOptionsAndPartialProgress(t *testing.T) {
	req := admindto.ReplayDLQRequest{SourceTopic: "events_dlq", TargetOverride: "events", Group: "replay", Since: "2026-10-01T00:00:00Z", Max: 10, MaxReplays: 2, DryRun: true}
	svc := &dlqService{replay: func(ctx context.Context, _ *config.Configuration, _ *logger.Logger, opts dlq.Options) (*dlq.Summary, error) {
		require.Equal(t, dlq.Options{SourceTopic: req.SourceTopic, TargetOverride: req.TargetOverride, Group: req.Group, SinceMs: 1790812800000, Max: 10, MaxReplays: 2, DryRun: true}, opts)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.WithinDuration(t, time.Now().Add(10*time.Minute), deadline, time.Second)
		return &dlq.Summary{Scanned: 5, Replayed: 3}, errors.New("broker unavailable")
	}}
	resp, err := svc.Preview(context.Background(), req)
	require.Error(t, err)
	require.Equal(t, 5, resp.Scanned)
	require.Equal(t, 3, resp.Replayed)
}

func TestDLQReplayCancellationAndSuccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := &dlqService{replay: func(ctx context.Context, _ *config.Configuration, _ *logger.Logger, opts dlq.Options) (*dlq.Summary, error) {
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		require.True(t, opts.FromStart)
		require.True(t, opts.DryRun)
		return &dlq.Summary{Replayed: 2}, nil
	}}
	resp, err := svc.Preview(ctx, admindto.ReplayDLQRequest{SourceTopic: "events_dlq", FromStart: true})
	require.NoError(t, err)
	require.Equal(t, 2, resp.Replayed)
}

type fakeRuns struct {
	t           *testing.T
	failOnStart bool
	startErr    error
	startedID   string
	started     dlqmodels.ReplayDLQWorkflowInput
	state       *replayRunState
	res         *dlqmodels.ReplayDLQWorkflowResult
	resultErr   error
}

func (f *fakeRuns) start(_ context.Context, id string, in dlqmodels.ReplayDLQWorkflowInput) (string, error) {
	if f.failOnStart {
		f.t.Fatal("invalid request reached Temporal")
	}
	f.startedID, f.started = id, in
	return "run-1", f.startErr
}

func (f *fakeRuns) describe(context.Context, string, string) (*replayRunState, error) {
	if f.state == nil {
		return nil, serviceerror.NewNotFound("missing")
	}
	return f.state, nil
}

func (f *fakeRuns) result(context.Context, string, string) (*dlqmodels.ReplayDLQWorkflowResult, error) {
	return f.res, f.resultErr
}

func TestDLQStartReplay(t *testing.T) {
	runs := &fakeRuns{}
	svc := &dlqService{ServiceParams: service.ServiceParams{Logger: logger.NewNoopLogger()}, runs: runs}
	run, err := svc.StartReplay(context.Background(), admindto.ReplayDLQRequest{SourceTopic: "events_dlq", Since: "2026-10-01T00:00:00Z", Max: 10, MaxReplays: 2})
	require.NoError(t, err)
	require.Equal(t, "dlq-replay-events_dlq:dlq-replay-tool", run.WorkflowID)
	require.Equal(t, admindto.ReplayStatusQueued, run.Status)
	require.Equal(t, dlqmodels.ReplayDLQWorkflowInput{SourceTopic: "events_dlq", SinceMs: 1790812800000, Max: 10, MaxReplays: 2}, runs.started)

	runs.startErr = serviceerror.NewWorkflowExecutionAlreadyStarted("running", "", "")
	_, err = svc.StartReplay(context.Background(), admindto.ReplayDLQRequest{SourceTopic: "events_dlq", Group: "mine"})
	require.Equal(t, "dlq-replay-events_dlq:mine", runs.startedID)
	require.True(t, ierr.IsAlreadyExists(err))
}

func TestDLQGetReplay(t *testing.T) {
	running := enums.WORKFLOW_EXECUTION_STATUS_RUNNING
	for _, tt := range []struct {
		name    string
		state   *replayRunState
		want    admindto.ReplayStatus
		summary bool
	}{
		{name: "no worker yet", state: &replayRunState{status: running}, want: admindto.ReplayStatusQueued},
		{name: "activity started", state: &replayRunState{status: running, activityStarted: true, attempt: 1}, want: admindto.ReplayStatusRunning},
		{name: "retry backoff", state: &replayRunState{status: running, attempt: 2, lastFailure: "broker down"}, want: admindto.ReplayStatusRunning},
		{name: "completed", state: &replayRunState{status: enums.WORKFLOW_EXECUTION_STATUS_COMPLETED}, want: admindto.ReplayStatusCompleted, summary: true},
		{name: "failed", state: &replayRunState{status: enums.WORKFLOW_EXECUTION_STATUS_FAILED}, want: admindto.ReplayStatusFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			runs := &fakeRuns{state: tt.state, res: &dlqmodels.ReplayDLQWorkflowResult{Replayed: 4}}
			if tt.want == admindto.ReplayStatusFailed {
				runs.res, runs.resultErr = nil, errors.New("activity failed")
			}
			run, err := (&dlqService{runs: runs}).GetReplay(context.Background(), "wf", "")
			require.NoError(t, err)
			require.Equal(t, tt.want, run.Status)
			require.Equal(t, tt.summary, run.Summary != nil)
			if tt.want == admindto.ReplayStatusFailed {
				require.Equal(t, "activity failed", run.LastError)
			}
		})
	}

	_, err := (&dlqService{runs: &fakeRuns{}}).GetReplay(context.Background(), "missing", "")
	require.True(t, ierr.IsNotFound(err))
}
