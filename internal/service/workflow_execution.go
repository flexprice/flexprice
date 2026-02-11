package service

import (
	"context"
	"math"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/workflowexecution"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
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
func (s *WorkflowExecutionService) CreateWorkflowExecution(ctx context.Context, input *CreateWorkflowExecutionInput) (*ent.WorkflowExecution, error) {
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

	// Create workflow execution entity
	now := time.Now().UTC()
	exec := &ent.WorkflowExecution{
		WorkflowID:    input.WorkflowID,
		RunID:         input.RunID,
		WorkflowType:  input.WorkflowType,
		TaskQueue:     input.TaskQueue,
		StartTime:     now,
		TenantID:      input.TenantID,
		EnvironmentID: input.EnvironmentID,
		CreatedBy:     input.CreatedBy,
		UpdatedBy:     input.CreatedBy,
		Metadata:      input.Metadata,
		Status:        string(types.StatusPublished),
		CreatedAt:     now,
		UpdatedAt:     now,

		Entity:         stringPtr(input.Entity),
		EntityID:       stringPtr(input.EntityID),
		WorkflowStatus: types.WorkflowExecutionStatusRunning,
	}

	if err := s.workflowExecRepo.Create(ctx, exec); err != nil {
		return nil, err
	}

	return exec, nil
}

// ListWorkflowExecutionsFilters represents filters for listing workflow executions
type ListWorkflowExecutionsFilters struct {
	TenantID      string
	EnvironmentID string
	WorkflowType  string
	TaskQueue     string
	PageSize      int
	Page          int
}

// ListWorkflowExecutions retrieves a paginated list of workflow executions
func (s *WorkflowExecutionService) ListWorkflowExecutions(ctx context.Context, filters *ListWorkflowExecutionsFilters) ([]*ent.WorkflowExecution, int64, error) {
	repoFilter := &workflowexecution.ListFilter{
		TenantID:      filters.TenantID,
		EnvironmentID: filters.EnvironmentID,
		WorkflowType:  filters.WorkflowType,
		TaskQueue:     filters.TaskQueue,
		PageSize:      filters.PageSize,
		Page:          filters.Page,
	}

	executions, total, err := s.workflowExecRepo.List(ctx, repoFilter)
	if err != nil {
		return nil, 0, err
	}

	return executions, int64(total), nil
}

// GetWorkflowExecution retrieves a single workflow execution by workflow_id and run_id
func (s *WorkflowExecutionService) GetWorkflowExecution(ctx context.Context, workflowID, runID string) (*ent.WorkflowExecution, error) {
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

// stringPtr returns a pointer to s, or nil if s is empty (for optional ent fields).
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
