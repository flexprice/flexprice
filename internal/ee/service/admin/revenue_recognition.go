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

// RevenueRecognitionService rebuilds an environment's revenue recognition from history.
type RevenueRecognitionService interface {
	// StartBackfill starts the environment's backfill; one runs at a time, and only while revenue recognition is on.
	StartBackfill(ctx context.Context, req admindto.StartRevenueRecognitionBackfillRequest) (*admindto.RevenueRecognitionBackfillRun, error)
	// GetBackfill reports a backfill's state.
	GetBackfill(ctx context.Context, workflowID, runID string) (*admindto.RevenueRecognitionBackfillRun, error)
}

// backfillRuns is the slice of Temporal the backfill needs; like the DLQ replay it uses the raw client.
type backfillRuns interface {
	start(ctx context.Context, workflowID string, input tmodels.RevenueRecognitionBackfillInput) (runID string, err error)
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

func (s *revenueRecognitionService) StartBackfill(ctx context.Context, req admindto.StartRevenueRecognitionBackfillRequest) (*admindto.RevenueRecognitionBackfillRun, error) {
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

	return s.startBackfill(ctx, req.TenantID, req.EnvironmentID)
}

func (s *revenueRecognitionService) GetBackfill(ctx context.Context, workflowID, runID string) (*admindto.RevenueRecognitionBackfillRun, error) {
	state, err := s.runs.describe(ctx, workflowID, runID)
	if _, ok := stderrors.AsType[*serviceerror.NotFound](err); ok {
		return nil, ierr.WithError(err).WithHint("No backfill found with this workflow ID").Mark(ierr.ErrNotFound)
	}
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not read the backfill workflow").Mark(ierr.ErrInternal)
	}

	return &admindto.RevenueRecognitionBackfillRun{
		WorkflowID: workflowID, RunID: state.runID, Status: runStatus(state),
		Attempt: state.attempt, LastError: state.lastFailure, StartedAt: state.startedAt, ClosedAt: state.closedAt,
	}, nil
}

// startBackfill starts the environment's one backfill run.
func (s *revenueRecognitionService) startBackfill(ctx context.Context, tenantID, environmentID string) (*admindto.RevenueRecognitionBackfillRun, error) {
	workflowID := "revenue-recognition-backfill-" + environmentID
	runID, err := s.runs.start(ctx, workflowID, tmodels.RevenueRecognitionBackfillInput{TenantID: tenantID, EnvironmentID: environmentID})
	if _, ok := stderrors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
		return nil, ierr.WithError(err).WithHint("A backfill is already running for this environment").
			WithReportableDetails(map[string]interface{}{"workflow_id": workflowID}).Mark(ierr.ErrAlreadyExists)
	}
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not start the backfill workflow").Mark(ierr.ErrInternal)
	}

	s.Logger.Info(ctx, "revenue recognition backfill started", "workflow_id", workflowID, "run_id", runID, "tenant_id", tenantID, "environment_id", environmentID)
	return &admindto.RevenueRecognitionBackfillRun{WorkflowID: workflowID, RunID: runID, Status: admindto.ReplayStatusQueued}, nil
}

type temporalBackfillRuns struct{ temporalReplayRuns }

func (r *temporalBackfillRuns) start(ctx context.Context, workflowID string, input tmodels.RevenueRecognitionBackfillInput) (string, error) {
	run, err := r.client.StartWorkflow(ctx, tmodels.StartWorkflowOptions{
		ID:                                       workflowID,
		TaskQueue:                                types.TemporalRevenueRecognitionBackfillWorkflow.TaskQueueName(),
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}, types.TemporalRevenueRecognitionBackfillWorkflow.String(), input)
	if err != nil {
		return "", err
	}

	return run.GetRunID(), nil
}
