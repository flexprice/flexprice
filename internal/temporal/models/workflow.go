package models

import (
	"context"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
)

// StartWorkflowOptions contains options for starting a workflow
type StartWorkflowOptions struct {
	// ID is the business identifier of the workflow execution
	ID string
	// TaskQueue is the task queue name to use for workflow tasks
	TaskQueue string
	// WorkflowExecutionTimeout is the timeout for the entire workflow execution
	WorkflowExecutionTimeout time.Duration
	// WorkflowRunTimeout is the timeout for a single workflow run
	WorkflowRunTimeout time.Duration
	// WorkflowTaskTimeout is the timeout for processing workflow task from the time the worker
	// pulled this task
	WorkflowTaskTimeout time.Duration
	// RetryPolicy specifies how to retry workflow execution
	RetryPolicy *RetryPolicy
}

// ToSDKOptions converts StartWorkflowOptions to Temporal SDK client.StartWorkflowOptions
func (o StartWorkflowOptions) ToSDKOptions() client.StartWorkflowOptions {
	return client.StartWorkflowOptions{
		ID:                       o.ID,
		TaskQueue:                o.TaskQueue,
		WorkflowExecutionTimeout: o.WorkflowExecutionTimeout,
		WorkflowRunTimeout:       o.WorkflowRunTimeout,
		WorkflowTaskTimeout:      o.WorkflowTaskTimeout,
		RetryPolicy:              o.RetryPolicy.ToSDKRetryPolicy(),
	}
}

// WorkflowRun represents a workflow execution
type WorkflowRun interface {
	// GetID returns the workflow ID
	GetID() string
	// GetRunID returns the workflow run ID
	GetRunID() string
	// Get blocks until workflow completes and returns its result
	Get(ctx context.Context, valuePtr interface{}) error
}

// workflowRunImpl implements WorkflowRun
type workflowRunImpl struct {
	run client.WorkflowRun
}

// NewWorkflowRun creates a new WorkflowRun from a Temporal SDK WorkflowRun
func NewWorkflowRun(run client.WorkflowRun) WorkflowRun {
	return &workflowRunImpl{run: run}
}

// GetID implements WorkflowRun
func (w *workflowRunImpl) GetID() string {
	return w.run.GetID()
}

// GetRunID implements WorkflowRun
func (w *workflowRunImpl) GetRunID() string {
	return w.run.GetRunID()
}

// Get implements WorkflowRun
func (w *workflowRunImpl) Get(ctx context.Context, valuePtr interface{}) error {
	return w.run.Get(ctx, valuePtr)
}

// RetryPolicy defines how to retry workflow execution
type RetryPolicy struct {
	// InitialInterval is the initial interval between retries
	InitialInterval time.Duration
	// BackoffCoefficient is the coefficient to multiply the interval by for each retry
	BackoffCoefficient float64
	// MaximumInterval is the maximum interval between retries
	MaximumInterval time.Duration
	// MaximumAttempts is the maximum number of attempts
	MaximumAttempts int32
	// NonRetryableErrorTypes specifies error types that shouldn't be retried
	NonRetryableErrorTypes []string
}

// WorkflowTimeout defines timeout settings for workflow execution
type WorkflowTimeout struct {
	// ExecutionTimeout is the timeout for the entire workflow execution
	ExecutionTimeout time.Duration
	// RunTimeout is the timeout for a single workflow run
	RunTimeout time.Duration
	// TaskTimeout is the timeout for processing workflow task
	TaskTimeout time.Duration
}

// ToSDKRetryPolicy converts RetryPolicy to Temporal SDK temporal.RetryPolicy
func (p *RetryPolicy) ToSDKRetryPolicy() *temporal.RetryPolicy {
	if p == nil {
		return nil
	}
	return &temporal.RetryPolicy{
		InitialInterval:        p.InitialInterval,
		BackoffCoefficient:     p.BackoffCoefficient,
		MaximumInterval:        p.MaximumInterval,
		MaximumAttempts:        p.MaximumAttempts,
		NonRetryableErrorTypes: p.NonRetryableErrorTypes,
	}
}

type TemporalWorkflowResult struct {
	Message    string `json:"message"`
	WorkflowID string `json:"workflow_id"`
	RunID      string `json:"run_id"`
}
