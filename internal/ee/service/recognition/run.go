package recognition

import (
	"context"
	stderrors "errors"
	"strings"

	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/settings"
	ierr "github.com/flexprice/flexprice/internal/errors"
	contractRepo "github.com/flexprice/flexprice/internal/repository/contract"
	tmodels "github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
)

// runWorkflowPrefix starts every recognition run's workflow ID; the environment follows, so one environment-wide run
// (tenant or admin) runs at a time.
const runWorkflowPrefix = "revenue-recognition-run-"

// RunService starts revenue recognition runs for the caller's environment and reports on them.
type RunService interface {
	// StartRun starts a run for one contract, one customer, or the whole environment; one runs at a time per scope.
	StartRun(ctx context.Context, req *dto.StartRevenueRecognitionRunRequest) (*dto.RevenueRecognitionRunResponse, error)
	// GetRun reports a run of the caller's environment.
	GetRun(ctx context.Context, workflowID string) (*dto.RevenueRecognitionRunResponse, error)
}

type runService struct {
	temporal     temporalservice.TemporalService
	contractRepo contractRepo.Repository
	customerRepo customer.Repository
	settingsRepo settings.Repository
}

func NewRunService(
	temporal temporalservice.TemporalService,
	contractRepo contractRepo.Repository,
	customerRepo customer.Repository,
	settingsRepo settings.Repository,
) RunService {
	return &runService{temporal: temporal, contractRepo: contractRepo, customerRepo: customerRepo, settingsRepo: settingsRepo}
}

func (s *runService) StartRun(ctx context.Context, req *dto.StartRevenueRecognitionRunRequest) (*dto.RevenueRecognitionRunResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if err := s.requireEnabled(ctx); err != nil {
		return nil, err
	}

	// e.g. revenue-recognition-run-env_1, or …-contract-contract_7 for one contract
	workflowID := runWorkflowPrefix + types.GetEnvironmentID(ctx)
	switch {
	case req.ContractID != "":
		if _, err := s.contractRepo.GetByID(ctx, req.ContractID); err != nil {
			return nil, err
		}
		workflowID += "-contract-" + req.ContractID
	case req.CustomerID != "":
		if _, err := s.customerRepo.Get(ctx, req.CustomerID); err != nil {
			return nil, err
		}
		workflowID += "-customer-" + req.CustomerID
	}

	run, err := s.temporal.StartWorkflow(ctx, tmodels.StartWorkflowOptions{
		ID:                                       workflowID,
		TaskQueue:                                types.TemporalRevenueRecognitionWorkflow.TaskQueueName(),
		WorkflowExecutionErrorWhenAlreadyStarted: true,
	}, types.TemporalRevenueRecognitionWorkflow, tmodels.RevenueRecognitionInput{
		TenantID: types.GetTenantID(ctx), EnvironmentID: types.GetEnvironmentID(ctx), ContractID: req.ContractID, CustomerID: req.CustomerID,
	})
	if _, ok := stderrors.AsType[*serviceerror.WorkflowExecutionAlreadyStarted](err); ok {
		return nil, ierr.WithError(err).WithHint("A recognition run for this scope is already running; wait for it to finish").
			WithReportableDetails(map[string]any{"workflow_id": workflowID}).Mark(ierr.ErrAlreadyExists)
	}
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not start the recognition run").Mark(ierr.ErrInternal)
	}

	return &dto.RevenueRecognitionRunResponse{WorkflowID: workflowID, RunID: run.GetRunID(), Status: dto.RevenueRecognitionRunStatusQueued}, nil
}

func (s *runService) GetRun(ctx context.Context, workflowID string) (*dto.RevenueRecognitionRunResponse, error) {
	envPrefix := runWorkflowPrefix + types.GetEnvironmentID(ctx)
	if workflowID != envPrefix && !strings.HasPrefix(workflowID, envPrefix+"-") {
		return nil, runNotFound(workflowID)
	}

	resp, err := s.temporal.DescribeWorkflowExecution(ctx, workflowID, "")
	if _, ok := stderrors.AsType[*serviceerror.NotFound](err); ok {
		return nil, runNotFound(workflowID)
	}
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Could not read the recognition run").Mark(ierr.ErrInternal)
	}
	desc, ok := resp.(*workflowservice.DescribeWorkflowExecutionResponse)
	if !ok {
		return nil, ierr.NewError("unexpected workflow description").Mark(ierr.ErrInternal)
	}
	return runResponse(workflowID, desc), nil
}

// requireEnabled refuses a run while revenue recognition is off for the environment.
func (s *runService) requireEnabled(ctx context.Context) error {
	setting, err := s.settingsRepo.GetByKey(ctx, types.SettingKeyRevenueRecognitionConfig)
	if err != nil && !ierr.IsNotFound(err) {
		return err
	}
	if setting != nil {
		if cfg, err := utils.ToStruct[types.RevenueRecognitionConfig](setting.Value); err == nil && cfg.Enabled {
			return nil
		}
	}
	return ierr.NewError("revenue recognition is disabled for this environment").
		WithHint("Enable revenue recognition before running it").
		Mark(ierr.ErrValidation)
}

// runResponse reports a run: queued until a worker starts its activity, then running, completed or failed.
func runResponse(workflowID string, desc *workflowservice.DescribeWorkflowExecutionResponse) *dto.RevenueRecognitionRunResponse {
	info := desc.GetWorkflowExecutionInfo()
	resp := &dto.RevenueRecognitionRunResponse{WorkflowID: workflowID, RunID: info.GetExecution().GetRunId()}
	if info.GetStartTime() != nil {
		resp.StartedAt = lo.ToPtr(info.GetStartTime().AsTime())
	}
	if info.GetCloseTime() != nil {
		resp.ClosedAt = lo.ToPtr(info.GetCloseTime().AsTime())
	}

	started := false
	for _, activity := range desc.GetPendingActivities() {
		started = activity.GetState() == enums.PENDING_ACTIVITY_STATE_STARTED || activity.GetAttempt() > 1
		resp.LastError = activity.GetLastFailure().GetMessage()
	}

	switch info.GetStatus() {
	case enums.WORKFLOW_EXECUTION_STATUS_RUNNING:
		resp.Status = dto.RevenueRecognitionRunStatusQueued
		if started {
			resp.Status = dto.RevenueRecognitionRunStatusRunning
		}
	case enums.WORKFLOW_EXECUTION_STATUS_COMPLETED:
		resp.Status = dto.RevenueRecognitionRunStatusCompleted
	default:
		resp.Status = dto.RevenueRecognitionRunStatusFailed
	}
	return resp
}

func runNotFound(workflowID string) error {
	return ierr.NewError("recognition run not found").
		WithHint("No recognition run of this environment has this workflow ID").
		WithReportableDetails(map[string]any{"workflow_id": workflowID}).
		Mark(ierr.ErrNotFound)
}
