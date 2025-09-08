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

// DefaultTemporalWorkflowOptions returns default workflow options
func DefaultTemporalWorkflowOptions() *TemporalWorkflowExecutionOptions {
	return &TemporalWorkflowExecutionOptions{
		ExecutionTimeout: defaultExecutionTimeout,
		RetryPolicy:      DefaultTemporalRetryPolicy(),
	}
}

// DefaultTemporalRetryPolicy returns default retry policy
func DefaultTemporalRetryPolicy() *TemporalRetryPolicy {
	return &TemporalRetryPolicy{
		InitialInterval:    defaultInitialInterval,
		BackoffCoefficient: defaultBackoffCoefficient,
		MaximumInterval:    defaultMaximumInterval,
		MaximumAttempts:    defaultMaximumAttempts,
	}
}

// DefaultTemporalStartWorkflowOptions returns default start workflow options
func DefaultTemporalStartWorkflowOptions() *TemporalStartWorkflowOptions {
	return &TemporalStartWorkflowOptions{
		ExecutionTimeout: defaultExecutionTimeout,
		RetryPolicy:      DefaultTemporalRetryPolicy(),
	}
}

// NewTemporalWorkflowOptions creates a new workflow options with custom settings
func NewTemporalWorkflowOptions(taskQueue string, timeout time.Duration) *TemporalWorkflowExecutionOptions {
	if timeout <= 0 {
		timeout = defaultExecutionTimeout
	}
	return &TemporalWorkflowExecutionOptions{
		TaskQueue:        taskQueue,
		ExecutionTimeout: timeout,
		RetryPolicy:      DefaultTemporalRetryPolicy(),
	}
}

// NewTemporalRetryPolicy creates a new retry policy with custom settings
func NewTemporalRetryPolicy(initialInterval time.Duration, backoffCoefficient float64, maxInterval time.Duration, maxAttempts int32) *TemporalRetryPolicy {
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

	return &TemporalRetryPolicy{
		InitialInterval:    initialInterval,
		BackoffCoefficient: backoffCoefficient,
		MaximumInterval:    maxInterval,
		MaximumAttempts:    maxAttempts,
	}
}

// WithNonRetryableErrors adds non-retryable error types to the retry policy
func (rp *TemporalRetryPolicy) WithNonRetryableErrors(errorTypes ...string) *TemporalRetryPolicy {
	rp.NonRetryableErrorTypes = append(rp.NonRetryableErrorTypes, errorTypes...)
	return rp
}

// Validate validates the retry policy
func (rp *TemporalRetryPolicy) Validate() error {
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
