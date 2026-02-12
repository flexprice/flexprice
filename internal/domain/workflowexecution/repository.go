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
	TenantID       string
	EnvironmentID  string
	WorkflowID     string // Filter by specific workflow ID
	WorkflowType   string
	TaskQueue      string
	WorkflowStatus string // e.g. Running, Completed, Failed

	// Entity columns for efficient filtering (replaces metadata JSONB filter)
	Entity   string // Filter by entity type (e.g. plan, invoice, subscription)
	EntityID string // Filter by entity ID (e.g. plan_01ABC, inv_xyz)

	// Sorting
	Sort  string // e.g. start_time, end_time, created_at
	Order string // asc | desc

	// Pagination (limit/offset style)
	Limit  int
	Offset int

	PageSize int
	Page     int
}
