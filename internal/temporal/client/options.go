package client

import (
	"fmt"
	"time"
)

const (
	defaultInitialInterval    = time.Second
	defaultBackoffCoefficient = 2.0
	defaultMaximumInterval    = time.Minute * 5
	defaultMaximumAttempts    = 3
	defaultExecutionTimeout   = time.Hour
)

// DefaultWorkflowOptions returns default workflow options
func DefaultWorkflowOptions() *WorkflowOptions {
	return &WorkflowOptions{
		ExecutionTimeout: defaultExecutionTimeout,
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
		ExecutionTimeout: defaultExecutionTimeout,
		RetryPolicy:      DefaultRetryPolicy(),
	}
}

// NewWorkflowOptions creates a new workflow options with custom settings
func NewWorkflowOptions(taskQueue string, timeout time.Duration) *WorkflowOptions {
	if timeout <= 0 {
		timeout = defaultExecutionTimeout
	}
	return &WorkflowOptions{
		TaskQueue:        taskQueue,
		ExecutionTimeout: timeout,
		RetryPolicy:      DefaultRetryPolicy(),
	}
}

// NewRetryPolicy creates a new retry policy with custom settings
func NewRetryPolicy(initialInterval time.Duration, backoffCoefficient float64, maxInterval time.Duration, maxAttempts int32) *RetryPolicy {
	if initialInterval <= 0 {
		initialInterval = defaultInitialInterval
	}
	if backoffCoefficient <= 1.0 {
		backoffCoefficient = defaultBackoffCoefficient
	}
	if maxInterval <= 0 {
		maxInterval = defaultMaximumInterval
	}
	if maxAttempts <= 0 {
		maxAttempts = defaultMaximumAttempts
	}

	return &RetryPolicy{
		InitialInterval:    initialInterval,
		BackoffCoefficient: backoffCoefficient,
		MaximumInterval:    maxInterval,
		MaximumAttempts:    maxAttempts,
	}
}

// WithNonRetryableErrors adds non-retryable error types to the retry policy
func (rp *RetryPolicy) WithNonRetryableErrors(errorTypes ...string) *RetryPolicy {
	rp.NonRetryableErrorTypes = append(rp.NonRetryableErrorTypes, errorTypes...)
	return rp
}

// Validate validates the retry policy
func (rp *RetryPolicy) Validate() error {
	if rp.InitialInterval <= 0 {
		return fmt.Errorf("initial interval must be positive")
	}
	if rp.BackoffCoefficient <= 1.0 {
		return fmt.Errorf("backoff coefficient must be greater than 1.0")
	}
	if rp.MaximumInterval <= 0 {
		return fmt.Errorf("maximum interval must be positive")
	}
	if rp.MaximumAttempts <= 0 {
		return fmt.Errorf("maximum attempts must be positive")
	}
	return nil
}
