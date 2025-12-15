package models

import (
	"context"
	"time"

	"go.temporal.io/sdk/client"
)

// WorkflowRun represents a running workflow
type WorkflowRun interface {
	// GetID returns the workflow ID
	GetID() string
	// GetRunID returns the workflow run ID
	GetRunID() string
	// Get blocks until the workflow completes and returns the result
	Get(ctx context.Context, valuePtr interface{}) error
}

// workflowRun wraps the SDK workflow run
type workflowRun struct {
	run client.WorkflowRun
}

// NewWorkflowRun creates a new workflow run wrapper
func NewWorkflowRun(run client.WorkflowRun) WorkflowRun {
	return &workflowRun{
		run: run,
	}
}

// GetID returns the workflow ID
func (w *workflowRun) GetID() string {
	return w.run.GetID()
}

// GetRunID returns the workflow run ID
func (w *workflowRun) GetRunID() string {
	return w.run.GetRunID()
}

// Get blocks until the workflow completes and returns the result
func (w *workflowRun) Get(ctx context.Context, valuePtr interface{}) error {
	return w.run.Get(ctx, valuePtr)
}

// StartWorkflowOptions represents options for starting a workflow
type StartWorkflowOptions struct {
	// ID is the workflow ID
	ID string
	// TaskQueue is the task queue name
	TaskQueue string
	// WorkflowExecutionTimeout is the timeout for the entire workflow execution
	WorkflowExecutionTimeout time.Duration
	// WorkflowRunTimeout is the timeout for a single workflow run
	WorkflowRunTimeout time.Duration
	// WorkflowTaskTimeout is the timeout for workflow task processing
	WorkflowTaskTimeout time.Duration
}

// ToSDKOptions converts StartWorkflowOptions to Temporal SDK client.StartWorkflowOptions
func (o *StartWorkflowOptions) ToSDKOptions() client.StartWorkflowOptions {
	return client.StartWorkflowOptions{
		ID:                       o.ID,
		TaskQueue:                o.TaskQueue,
		WorkflowExecutionTimeout: o.WorkflowExecutionTimeout,
		WorkflowRunTimeout:       o.WorkflowRunTimeout,
		WorkflowTaskTimeout:      o.WorkflowTaskTimeout,
	}
}

// TemporalWorkflowResult represents the result of starting a Temporal workflow
type TemporalWorkflowResult struct {
	Message    string `json:"message"`
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
}
