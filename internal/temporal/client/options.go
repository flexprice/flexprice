package client

import "time"

const (
	defaultInitialInterval    = time.Second
	defaultBackoffCoefficient = 2.0
	defaultMaximumInterval    = time.Minute * 5
	defaultMaximumAttempts    = 3
)

// DefaultWorkflowOptions returns default workflow options
func DefaultWorkflowOptions() *WorkflowOptions {
	return &WorkflowOptions{
		ExecutionTimeout: time.Hour,
		RetryPolicy:      DefaultRetryPolicy(),
	}
}

// DefaultRetryPolicy returns default retry policy
func DefaultRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		InitialInterval:    defaultInitialInterval,
		BackoffCoefficient: defaultBackoffCoefficient,
		MaximumInterval:    defaultMaximumInterval,
		MaximumAttempts:    defaultMaximumAttempts,
	}
}

// DefaultStartWorkflowOptions returns default start workflow options
func DefaultStartWorkflowOptions() *StartWorkflowOptions {
	return &StartWorkflowOptions{
		ExecutionTimeout: time.Hour,
	}
}
