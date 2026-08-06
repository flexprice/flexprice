package service

import (
	"context"
	"math"
	"time"

	"github.com/flexprice/flexprice/internal/domain/workflowexecution"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// WorkflowExecutionService provides methods for managing workflow execution records
type WorkflowExecutionService struct {
	ServiceParams
	workflowExecRepo workflowexecution.Repository
}

// NewWorkflowExecutionService creates a new WorkflowExecutionService instance
func NewWorkflowExecutionService(params ServiceParams, workflowExecRepo workflowexecution.Repository) *WorkflowExecutionService {
	return &WorkflowExecutionService{
		ServiceParams:    params,
		workflowExecRepo: workflowExecRepo,
	}
}

// CreateWorkflowExecutionInput represents the input for creating a workflow execution record
type CreateWorkflowExecutionInput struct {
	WorkflowID    string
	RunID         string
	WorkflowType  string
	TaskQueue     string
	TenantID      string
	EnvironmentID string
	CreatedBy     string
	Entity        string // e.g. plan, invoice, subscription (stored in column for efficient filtering)
	EntityID      string // e.g. plan ID, invoice ID
	Metadata      map[string]interface{}
}

// CreateWorkflowExecution creates a new workflow execution record in the database
func (s *WorkflowExecutionService) CreateWorkflowExecution(ctx context.Context, input *CreateWorkflowExecutionInput) (*workflowexecution.WorkflowExecution, error) {
	// Validate required fields
	if input.WorkflowID == "" {
		return nil, ierr.NewError("workflow_id is required").
			WithHint("Workflow ID must be provided").
			Mark(ierr.ErrValidation)
	}
	if input.RunID == "" {
		return nil, ierr.NewError("run_id is required").
			WithHint("Run ID must be provided").
			Mark(ierr.ErrValidation)
	}
	if input.WorkflowType == "" {
		return nil, ierr.NewError("workflow_type is required").
			WithHint("Workflow type must be provided").
			Mark(ierr.ErrValidation)
	}
	if input.TenantID == "" {
		return nil, ierr.NewError("tenant_id is required").
			WithHint("Tenant ID must be provided").
			Mark(ierr.ErrValidation)
	}

	// Create workflow execution domain model (workflow_status defaults to Running at start)
	now := time.Now().UTC()
	exec := &workflowexecution.WorkflowExecution{
		WorkflowID:     input.WorkflowID,
		RunID:          input.RunID,
		WorkflowType:   input.WorkflowType,
		TaskQueue:      input.TaskQueue,
		StartTime:      now,
		EnvironmentID:  input.EnvironmentID,
		Entity:         input.Entity,
		EntityID:       input.EntityID,
		Metadata:       input.Metadata,
		WorkflowStatus: types.WorkflowExecutionStatusRunning,
		BaseModel: types.BaseModel{
			TenantID:  input.TenantID,
			Status:    types.StatusPublished,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: input.CreatedBy,
			UpdatedBy: input.CreatedBy,
		},
	}

	if err := s.workflowExecRepo.Create(ctx, exec); err != nil {
		return nil, err
	}

	return exec, nil
}

// ListWorkflowExecutions retrieves a paginated list of workflow executions using the same filter format as features (Filters, Sort, QueryFilter, TimeRangeFilter).
func (s *WorkflowExecutionService) ListWorkflowExecutions(ctx context.Context, filter *types.WorkflowExecutionFilter) ([]*workflowexecution.WorkflowExecution, int64, error) {
	if filter == nil {
		filter = types.NewDefaultWorkflowExecutionFilter()
	}
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}
	if filter.QueryFilter.Sort == nil {
		filter.QueryFilter.Sort = lo.ToPtr("start_time")
		filter.QueryFilter.Order = lo.ToPtr("desc")
	}
	if err := filter.Validate(); err != nil {
		return nil, 0, err
	}

	executions, err := s.workflowExecRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.workflowExecRepo.Count(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	return executions, int64(total), nil
}

// GetWorkflowExecution retrieves a single workflow execution by workflow_id and run_id
func (s *WorkflowExecutionService) GetWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowexecution.WorkflowExecution, error) {
	return s.workflowExecRepo.Get(ctx, workflowID, runID)
}

// DeleteWorkflowExecution deletes a workflow execution record
func (s *WorkflowExecutionService) DeleteWorkflowExecution(ctx context.Context, id string) error {
	return s.workflowExecRepo.Delete(ctx, id)
}

// CalculateTotalPages calculates the total number of pages
func CalculateTotalPages(total int64, pageSize int) int {
	if pageSize <= 0 {
		return 0
	}
	return int(math.Ceil(float64(total) / float64(pageSize)))
}
