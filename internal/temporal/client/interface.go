package client

import (
	"context"
	"time"

	"go.temporal.io/sdk/client"
)

// TemporalClientInterface defines the interface for interacting with Temporal
type TemporalClientInterface interface {
	// Core workflow execution methods
	ExecuteWorkflow(ctx context.Context, workflowName string, input interface{}) (TemporalWorkflowRun, error)
	ExecuteWorkflowWithOptions(ctx context.Context, workflowName string, input interface{}, options *TemporalWorkflowExecutionOptions) (TemporalWorkflowRun, error)

	// Get underlying temporal client
	GetTemporalClient() client.Client

	// Client management
	Close() error
}

// TemporalWorkflowRun represents a running workflow instance
type TemporalWorkflowRun interface {
	GetID() string
	GetRunID() string
	Get(ctx context.Context, valuePtr interface{}) error
	GetWithTimeout(ctx context.Context, timeout time.Duration, valuePtr interface{}) error
}

// TemporalWorkflowExecutionOptions represents options for workflow execution
type TemporalWorkflowExecutionOptions struct {
	ID               string
	TaskQueue        string
	ExecutionTimeout time.Duration
	RetryPolicy      *TemporalRetryPolicy
}

// TemporalRetryPolicy defines how workflow/activity execution should be retried
type TemporalRetryPolicy struct {
	InitialInterval        time.Duration
	BackoffCoefficient     float64
	MaximumInterval        time.Duration
	MaximumAttempts        int32
	NonRetryableErrorTypes []string
}

// TemporalStartWorkflowOptions represents options for starting workflows
type TemporalStartWorkflowOptions struct {
	ExecutionTimeout time.Duration
	RetryPolicy      *TemporalRetryPolicy
}
