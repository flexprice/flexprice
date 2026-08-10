package workflowexecution

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

// WorkflowRef identifies a single workflow execution. Fields are unexported so
// a reference cannot be partially constructed or mutated after creation; build
// one with NewWorkflowRef and read it through the getters.
type WorkflowRef struct {
	workflowID string
	runID      string
}

// NewWorkflowRef returns a reference to one workflow execution.
func NewWorkflowRef(workflowID, runID string) WorkflowRef {
	return WorkflowRef{workflowID: workflowID, runID: runID}
}

// WorkflowID returns the Temporal workflow ID.
func (r WorkflowRef) WorkflowID() string { return r.workflowID }

// RunID returns the Temporal run ID.
func (r WorkflowRef) RunID() string { return r.runID }

// WorkflowExecution represents a workflow execution record in the domain layer.
// It mirrors the persisted entity but is independent of the ORM.
type WorkflowExecution struct {
	// ID is the unique identifier (ULID) for the record
	ID string `json:"id"`

	// WorkflowID is the user-provided workflow ID (Temporal)
	WorkflowID string `json:"workflow_id"`

	// RunID is the Temporal-generated UUID for this specific execution
	RunID string `json:"run_id"`

	// WorkflowType is the workflow type name (e.g. BillingWorkflow, PriceSyncWorkflow)
	WorkflowType string `json:"workflow_type"`

	// TaskQueue is the Temporal task queue name
	TaskQueue string `json:"task_queue"`

	// StartTime is when the workflow execution started
	StartTime time.Time `json:"start_time"`

	// EndTime is when the workflow completed or failed (optional until closed)
	EndTime *time.Time `json:"end_time,omitempty"`

	// DurationMs is the total execution duration in milliseconds (optional until closed)
	DurationMs *int64 `json:"duration_ms,omitempty"`

	// WorkflowStatus is the Temporal workflow run status (Running, Completed, Failed, etc.)
	WorkflowStatus types.WorkflowExecutionStatus `json:"workflow_status"`

	// Entity is the entity type for filtering (e.g. plan, invoice, subscription)
	Entity string `json:"entity,omitempty"`

	// EntityID is the entity ID for filtering (e.g. plan ID, invoice ID)
	EntityID string `json:"entity_id,omitempty"`

	// Metadata holds custom key-value data (e.g. customer_id, plan)
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// EnvironmentID is the environment identifier (from EnvironmentMixin)
	EnvironmentID string `json:"environment_id,omitempty"`

	types.BaseModel
}

// FromEnt converts an ent.WorkflowExecution to a domain WorkflowExecution.
func FromEnt(e *ent.WorkflowExecution) *WorkflowExecution {
	if e == nil {
		return nil
	}
	exec := &WorkflowExecution{
		ID:             e.ID,
		WorkflowID:     e.WorkflowID,
		RunID:          e.RunID,
		WorkflowType:   e.WorkflowType,
		TaskQueue:      e.TaskQueue,
		StartTime:      e.StartTime,
		EndTime:        e.EndTime,
		DurationMs:     e.DurationMs,
		WorkflowStatus: e.WorkflowStatus,
		Metadata:       e.Metadata,
		Entity:         stringPtrToStr(e.Entity),
		EntityID:       stringPtrToStr(e.EntityID),
		EnvironmentID:  e.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
		},
	}
	return exec
}

// FromEntList converts a slice of ent.WorkflowExecution to domain WorkflowExecution slice.
func FromEntList(ents []*ent.WorkflowExecution) []*WorkflowExecution {
	if len(ents) == 0 {
		return nil
	}
	out := make([]*WorkflowExecution, len(ents))
	for i, e := range ents {
		out[i] = FromEnt(e)
	}
	return out
}

// ToEnt converts a domain WorkflowExecution to an ent.WorkflowExecution for persistence.
// Used by the repository on Create. ID may be left empty for new records (DB will set it).
func ToEnt(exec *WorkflowExecution) *ent.WorkflowExecution {
	if exec == nil {
		return nil
	}
	e := &ent.WorkflowExecution{
		ID:             exec.ID,
		WorkflowID:     exec.WorkflowID,
		RunID:          exec.RunID,
		WorkflowType:   exec.WorkflowType,
		TaskQueue:      exec.TaskQueue,
		StartTime:      exec.StartTime,
		EndTime:        exec.EndTime,
		DurationMs:     exec.DurationMs,
		WorkflowStatus: exec.WorkflowStatus,
		Metadata:       exec.Metadata,
		TenantID:       exec.TenantID,
		EnvironmentID:  exec.EnvironmentID,
		Status:         string(exec.Status),
		CreatedAt:      exec.CreatedAt,
		UpdatedAt:      exec.UpdatedAt,
		CreatedBy:      exec.CreatedBy,
		UpdatedBy:      exec.UpdatedBy,
	}
	if exec.Entity != "" {
		e.Entity = &exec.Entity
	}
	if exec.EntityID != "" {
		e.EntityID = &exec.EntityID
	}
	return e
}

func stringPtrToStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
