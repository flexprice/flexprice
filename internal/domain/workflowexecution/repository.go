package workflowexecution

import (
	"context"

	"github.com/flexprice/flexprice/ent"
)

// Repository defines the interface for workflow execution data access
type Repository interface {
	Create(ctx context.Context, exec *ent.WorkflowExecution) error
	Get(ctx context.Context, workflowID, runID string) (*ent.WorkflowExecution, error)
	List(ctx context.Context, filter *ListFilter) ([]*ent.WorkflowExecution, int, error)
	Delete(ctx context.Context, id string) error
}

// ListFilter defines filters for listing workflow executions
type ListFilter struct {
	TenantID      string
	EnvironmentID string
	WorkflowType  string
	TaskQueue     string
	PageSize      int
	Page          int
}
