package admin

import (
	"context"
	stderrors "errors"
	"time"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/kafka/dlq"
	"github.com/flexprice/flexprice/internal/logger"
	temporalclient "github.com/flexprice/flexprice/internal/temporal/client"
	tmodels "github.com/flexprice/flexprice/internal/temporal/models"
	dlqmodels "github.com/flexprice/flexprice/internal/temporal/models/dlq"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
)

// DLQService replays regional dead letters for authenticated operators.
type DLQService interface {
	// Preview scans a DLQ without publishing or saving progress.
	Preview(ctx context.Context, req admindto.ReplayDLQRequest) (*admindto.ReplayDLQResponse, error)
	// StartReplay starts a background replay on the Temporal worker.
	StartReplay(ctx context.Context, req admindto.ReplayDLQRequest) (*admindto.ReplayDLQRun, error)
	// GetReplay reports a background replay's state and, once closed, its outcome.
	GetReplay(ctx context.Context, workflowID, runID string) (*admindto.ReplayDLQRun, error)
}

// replayRuns is the slice of Temporal the replay needs; the admin process has no tenant context,
// so it uses the raw client rather than TemporalService.
type replayRuns interface {
	start(ctx context.Context, workflowID string, input dlqmodels.ReplayDLQWorkflowInput) (runID string, err error)
	describe(ctx context.Context, workflowID, runID string) (*replayRunState, error)
	result(ctx context.Context, workflowID, runID string) (*dlqmodels.ReplayDLQWorkflowResult, error)
}

type replayRunState struct {
	runID           string
	status          enums.WorkflowExecutionStatus
	activityStarted bool
	attempt         int32
	lastFailure     string
	startedAt       *time.Time
	closedAt        *time.Time
}

type dlqService struct {
	service.ServiceParams
	replay func(context.Context, *config.Configuration, *logger.Logger, dlq.Options) (*dlq.Summary, error)
	runs   replayRuns
}

// NewDLQService constructs the admin replay service.
func NewDLQService(params service.ServiceParams, client temporalclient.TemporalClient) DLQService {
	return &dlqService{ServiceParams: params, replay: dlq.Replay, runs: &temporalReplayRuns{client: client}}
}

func (s *dlqService) Preview(ctx context.Context, req admindto.ReplayDLQRequest) (*admindto.ReplayDLQResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	opts := toOptions(req)
	opts.DryRun = true
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	summary, err := s.replay(ctx, s.Config, s.Logger, opts)
	resp := toResponse(summary)
	if err != nil {
		return resp, ierr.WithError(err).WithHint("Preview stopped before the end of the topic").WithReportableDetails(map[string]interface{}{"source_topic": req.SourceTopic, "summary": resp}).Mark(ierr.ErrInternal)
	}
	return resp, nil
}

func (s *dlqService) StartReplay(ctx context.Context, req admindto.ReplayDLQRequest) (*admindto.ReplayDLQRun, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	opts := toOptions(req)
	group := opts.Group
	if group == "" {
		group = dlq.DefaultGroup
	}
	// One run per topic and group: two runs would share saved progress and publish the same messages.
	// Kafka topic names cannot contain ':', so the first ':' always separates topic from group.
	workflowID := "dlq-replay-" + opts.SourceTopic + ":" + group
	input := dlqmodels.ReplayDLQWorkflowInput{
		SourceTopic: opts.SourceTopic, TargetOverride: opts.TargetOverride, Group: opts.Group, SinceMs: opts.SinceMs,
		FromStart: opts.FromStart, Max: opts.Max, MaxReplays: opts.MaxReplays,
	}
	runID, err := s.runs.start(ctx, workflowID, input)
	if _, ok := stderrors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
		return nil, ierr.WithError(err).WithHint("A replay is already running for this topic and group").WithReportableDetails(map[string]interface{}{"workflow_id": workflowID}).Mark(ierr.ErrAlreadyExists)
	}
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not start the replay workflow").Mark(ierr.ErrInternal)
	}
	s.Logger.Info(ctx, "dlq replay workflow started", "workflow_id", workflowID, "run_id", runID, "source", opts.SourceTopic, "group", group)
	return &admindto.ReplayDLQRun{WorkflowID: workflowID, RunID: runID, Status: admindto.ReplayStatusQueued}, nil
}

func (s *dlqService) GetReplay(ctx context.Context, workflowID, runID string) (*admindto.ReplayDLQRun, error) {
	state, err := s.runs.describe(ctx, workflowID, runID)
	if _, ok := stderrors.AsType[*serviceerror.NotFound](err); ok {
		return nil, ierr.WithError(err).WithHint("No replay found with this workflow ID").Mark(ierr.ErrNotFound)
	}
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not read the replay workflow").Mark(ierr.ErrInternal)
	}

	run := &admindto.ReplayDLQRun{
		WorkflowID: workflowID, RunID: state.runID, Status: runStatus(state),
		Attempt: state.attempt, LastError: state.lastFailure, StartedAt: state.startedAt, ClosedAt: state.closedAt,
	}
	if state.status == enums.WORKFLOW_EXECUTION_STATUS_RUNNING {
		return run, nil
	}
	result, err := s.runs.result(ctx, workflowID, state.runID)
	if err != nil {
		run.LastError = err.Error()
		return run, nil
	}
	run.Summary = &admindto.ReplayDLQResponse{
		Scanned: result.Scanned, Replayed: result.Replayed, Skipped: result.Skipped,
		Quarantined: result.Quarantined, ByTarget: result.ByTarget, ByReason: result.ByReason,
	}
	return run, nil
}

// runStatus is queued until a worker picks up the activity, so a region without a polling worker is visible.
func runStatus(state *replayRunState) admindto.ReplayStatus {
	switch state.status {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		if !state.activityStarted && state.attempt <= 1 {
			return admindto.ReplayStatusQueued
		}
		return admindto.ReplayStatusRunning
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		return admindto.ReplayStatusCompleted
	default:
		return admindto.ReplayStatusFailed
	}
}

func toOptions(req admindto.ReplayDLQRequest) dlq.Options {
	opts := dlq.Options{SourceTopic: req.SourceTopic, TargetOverride: req.TargetOverride, Group: req.Group, FromStart: req.FromStart, Max: req.Max, MaxReplays: req.MaxReplays}
	if req.Since != "" {
		since, _ := time.Parse(time.RFC3339, req.Since) // Validated by the caller.
		opts.SinceMs = since.UnixMilli()
	}
	return opts
}

func toResponse(summary *dlq.Summary) *admindto.ReplayDLQResponse {
	if summary == nil {
		return nil
	}
	return &admindto.ReplayDLQResponse{Scanned: summary.Scanned, Replayed: summary.Replayed, Skipped: summary.Skipped, Quarantined: summary.Quarantined, ByTarget: summary.ByTarget, ByReason: summary.ByReason}
}

type temporalReplayRuns struct{ client temporalclient.TemporalClient }

func (r *temporalReplayRuns) start(ctx context.Context, workflowID string, input dlqmodels.ReplayDLQWorkflowInput) (string, error) {
	run, err := r.client.StartWorkflow(ctx, tmodels.StartWorkflowOptions{
		ID:                                       workflowID,
		TaskQueue:                                types.TemporalReplayDLQWorkflow.TaskQueueName(),
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}, types.TemporalReplayDLQWorkflow.String(), input)
	if err != nil {
		return "", err
	}
	return run.GetRunID(), nil
}

func (r *temporalReplayRuns) describe(ctx context.Context, workflowID, runID string) (*replayRunState, error) {
	resp, err := r.client.DescribeWorkflowExecution(ctx, workflowID, runID)
	if err != nil {
		return nil, err
	}
	info := resp.GetWorkflowExecutionInfo()
	state := &replayRunState{
		runID:  info.GetExecution().GetRunId(),
		status: info.GetStatus(),
	}
	if info.GetStartTime() != nil {
		started := info.GetStartTime().AsTime()
		state.startedAt = &started
	}
	if info.GetCloseTime() != nil {
		closed := info.GetCloseTime().AsTime()
		state.closedAt = &closed
	}
	for _, activity := range resp.GetPendingActivities() {
		state.activityStarted = activity.GetState() == enums.PENDING_ACTIVITY_STATE_STARTED
		state.attempt = activity.GetAttempt()
		state.lastFailure = activity.GetLastFailure().GetMessage()
	}
	return state, nil
}

func (r *temporalReplayRuns) result(ctx context.Context, workflowID, runID string) (*dlqmodels.ReplayDLQWorkflowResult, error) {
	var result dlqmodels.ReplayDLQWorkflowResult
	if err := r.client.GetRawClient().GetWorkflow(ctx, workflowID, runID).Get(ctx, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
