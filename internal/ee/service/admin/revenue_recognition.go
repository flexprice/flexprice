package admin

import (
	"context"
	stderrors "errors"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	temporalclient "github.com/flexprice/flexprice/internal/temporal/client"
	tmodels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"go.temporal.io/api/serviceerror"
)

// RevenueRecognitionService rebuilds an environment's revenue recognition from history and runs recognition on demand.
type RevenueRecognitionService interface {
	// StartBackfill starts the environment's backfill; one runs at a time, and only while revenue recognition is on.
	StartBackfill(ctx context.Context, req admindto.StartRevenueRecognitionBackfillRequest) (*admindto.RevenueRecognitionRun, error)
	// StartRun starts a recognition run, limited to a tenant or environment when given; one runs at a time per scope.
	StartRun(ctx context.Context, req admindto.StartRevenueRecognitionRunRequest) (*admindto.RevenueRecognitionRun, error)
	// GetRun reports a backfill's or recognition run's state.
	GetRun(ctx context.Context, workflowID, runID string) (*admindto.RevenueRecognitionRun, error)
}

// backfillRuns is the slice of Temporal the revenue recognition jobs need; like the DLQ replay it uses the raw client.
type backfillRuns interface {
	start(ctx context.Context, workflowID string, workflow types.TemporalWorkflowType, input any) (runID string, err error)
	describe(ctx context.Context, workflowID, runID string) (*replayRunState, error)
}

type revenueRecognitionService struct {
	service.ServiceParams
	settings service.SettingsService
	runs     backfillRuns
}

func NewRevenueRecognitionService(params service.ServiceParams, settings service.SettingsService, client temporalclient.TemporalClient) RevenueRecognitionService {
	return &revenueRecognitionService{ServiceParams: params, settings: settings, runs: &temporalBackfillRuns{temporalReplayRuns{client: client}}}
}

func (s *revenueRecognitionService) StartBackfill(ctx context.Context, req admindto.StartRevenueRecognitionBackfillRequest) (*admindto.RevenueRecognitionRun, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	ctx, err := environmentContext(ctx, s.ServiceParams, req.TenantID, req.EnvironmentID)
	if err != nil {
		return nil, err
	}

	setting, err := s.SettingsRepo.GetByKey(ctx, types.SettingKeyRevenueRecognitionConfig)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}
	enabled := false
	if setting != nil {
		cfg, err := utils.ToStruct[types.RevenueRecognitionConfig](setting.Value)
		enabled = err == nil && cfg.Enabled
	}
	if !enabled {
		return nil, ierr.NewError("revenue recognition is disabled for this environment").
			WithHint("Enable revenue recognition before running a backfill").
			Mark(ierr.ErrValidation)
	}

	return s.startRun(ctx, "revenue-recognition-backfill-"+req.EnvironmentID, types.TemporalRevenueRecognitionBackfillWorkflow,
		tmodels.RevenueRecognitionBackfillInput{TenantID: req.TenantID, EnvironmentID: req.EnvironmentID})
}

func (s *revenueRecognitionService) StartRun(ctx context.Context, req admindto.StartRevenueRecognitionRunRequest) (*admindto.RevenueRecognitionRun, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	scope := "all"
	switch {
	case req.EnvironmentID != "":
		if _, err := environmentContext(ctx, s.ServiceParams, req.TenantID, req.EnvironmentID); err != nil {
			return nil, err
		}
		scope = req.EnvironmentID
	case req.TenantID != "":
		if _, err := s.TenantRepo.GetByID(ctx, req.TenantID); err != nil {
			return nil, err
		}
		scope = req.TenantID
	}

	return s.startRun(ctx, "revenue-recognition-run-"+scope, types.TemporalRevenueRecognitionWorkflow,
		tmodels.RevenueRecognitionInput{Since: req.Since, TenantID: req.TenantID, EnvironmentID: req.EnvironmentID})
}

func (s *revenueRecognitionService) GetRun(ctx context.Context, workflowID, runID string) (*admindto.RevenueRecognitionRun, error) {
	state, err := s.runs.describe(ctx, workflowID, runID)
	if _, ok := stderrors.AsType[*serviceerror.NotFound](err); ok {
		return nil, ierr.WithError(err).WithHint("No run found with this workflow ID").Mark(ierr.ErrNotFound)
	}
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not read the workflow").Mark(ierr.ErrInternal)
	}

	return &admindto.RevenueRecognitionRun{
		WorkflowID: workflowID, RunID: state.runID, Status: runStatus(state),
		Attempt: state.attempt, LastError: state.lastFailure, StartedAt: state.startedAt, ClosedAt: state.closedAt,
	}, nil
}

// startRun starts workflow under workflowID; while a run is open there, another is refused.
func (s *revenueRecognitionService) startRun(ctx context.Context, workflowID string, workflow types.TemporalWorkflowType, input any) (*admindto.RevenueRecognitionRun, error) {
	runID, err := s.runs.start(ctx, workflowID, workflow, input)
	if _, ok := stderrors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
		return nil, ierr.WithError(err).WithHint("This job is already running; wait for it to finish").
			WithReportableDetails(map[string]interface{}{"workflow_id": workflowID}).Mark(ierr.ErrAlreadyExists)
	}
	if err != nil {
		return nil, ierr.WithError(err).WithHintf("Could not start %s", workflow).Mark(ierr.ErrInternal)
	}

	s.Logger.Info(ctx, "revenue recognition job started", "workflow", workflow, "workflow_id", workflowID, "run_id", runID)
	return &admindto.RevenueRecognitionRun{WorkflowID: workflowID, RunID: runID, Status: admindto.ReplayStatusQueued}, nil
}

type temporalBackfillRuns struct{ temporalReplayRuns }

func (r *temporalBackfillRuns) start(ctx context.Context, workflowID string, workflow types.TemporalWorkflowType, input any) (string, error) {
	run, err := r.client.StartWorkflow(ctx, tmodels.StartWorkflowOptions{
		ID:                                       workflowID,
		TaskQueue:                                workflow.TaskQueueName(),
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}, workflow.String(), input)
	if err != nil {
		return "", err
	}

	return run.GetRunID(), nil
}
