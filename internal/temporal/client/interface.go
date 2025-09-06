package client

import (
	"context"
	"time"

	"go.temporal.io/sdk/client"
)

// TemporalClient defines the interface for interacting with Temporal
type TemporalClient interface {
	// Core workflow execution methods
	ExecuteWorkflow(ctx context.Context, workflowName string, input interface{}) (WorkflowRun, error)
	ExecuteWorkflowWithOptions(ctx context.Context, workflowName string, input interface{}, options *WorkflowOptions) (WorkflowRun, error)

	// Get underlying temporal client
	GetTemporalClient() client.Client

	// Client management
	Close() error
}

// WorkflowRun represents a running workflow instance
type WorkflowRun interface {
	GetID() string
	Get(ctx context.Context, valuePtr interface{}) error
	GetWithTimeout(ctx context.Context, timeout time.Duration, valuePtr interface{}) error
}

// WorkflowOptions represents options for workflow execution
type WorkflowOptions struct {
	ID               string
	TaskQueue        string
	ExecutionTimeout time.Duration
	RetryPolicy      *RetryPolicy
}

// RetryPolicy defines how workflow/activity execution should be retried
type RetryPolicy struct {
	InitialInterval        time.Duration
	BackoffCoefficient     float64
	MaximumInterval        time.Duration
	MaximumAttempts        int32
	NonRetryableErrorTypes []string
}
