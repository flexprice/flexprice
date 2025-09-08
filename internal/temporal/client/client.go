package client

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/client"
	temporalsdk "go.temporal.io/sdk/temporal"
)

var (
	globalClient     *temporal.TemporalClient
	globalClientOnce sync.Once
	globalClientMux  sync.RWMutex
)

// GetTemporalClient returns the global temporal client instance, creating it if necessary
func GetTemporalClient(cfg *config.TemporalConfig, log *logger.Logger) (*temporal.TemporalClient, error) {
	globalClientMux.RLock()
	if globalClient != nil {
		defer globalClientMux.RUnlock()
		return globalClient, nil
	}
	globalClientMux.RUnlock()

	var err error
	globalClientOnce.Do(func() {
		globalClient, err = temporal.NewTemporalClient(cfg, log)
	})
	return globalClient, err
}

// ExecuteWorkflow executes a temporal workflow with optional configuration
func ExecuteWorkflow(c *temporal.TemporalClient, ctx context.Context, workflowName string, input interface{}, options ...*TemporalStartWorkflowOptions) (client.WorkflowRun, error) {
	// Set default options if none provided
	var opts *TemporalStartWorkflowOptions
	if len(options) > 0 && options[0] != nil {
		opts = options[0]
	} else {
		opts = DefaultTemporalStartWorkflowOptions()
	}

	// Validate inputs
	if workflowName == "" {
		return nil, fmt.Errorf("workflow name is required")
	}
	if input == nil {
		return nil, fmt.Errorf("workflow input is required")
	}

	// Generate workflow ID
	workflowID := generateTemporalWorkflowID(workflowName, ctx)

	// Set default timeout if not provided
	if opts.ExecutionTimeout <= 0 {
		opts.ExecutionTimeout = time.Hour
	}

	// Build temporal workflow options
	temporalOptions := &client.StartWorkflowOptions{
		ID:                 workflowID,
		WorkflowRunTimeout: opts.ExecutionTimeout,
	}

	// Add retry policy if provided
	if opts.RetryPolicy != nil {
		if err := opts.RetryPolicy.Validate(); err != nil {
			return nil, fmt.Errorf("invalid retry policy: %w", err)
		}
		temporalOptions.RetryPolicy = &temporalsdk.RetryPolicy{
			InitialInterval:        opts.RetryPolicy.InitialInterval,
			BackoffCoefficient:     opts.RetryPolicy.BackoffCoefficient,
			MaximumInterval:        opts.RetryPolicy.MaximumInterval,
			MaximumAttempts:        opts.RetryPolicy.MaximumAttempts,
			NonRetryableErrorTypes: opts.RetryPolicy.NonRetryableErrorTypes,
		}
	}

	// Execute the workflow
	run, err := c.Client.ExecuteWorkflow(ctx, *temporalOptions, workflowName, input)
	if err != nil {
		return nil, fmt.Errorf("failed to execute workflow '%s': %w", workflowName, err)
	}

	return run, nil
}

// generateTemporalWorkflowID generates a unique workflow ID for temporal workflows
func generateTemporalWorkflowID(workflowName string, ctx context.Context) string {
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		tenantID = "unknown"
	}
	return fmt.Sprintf("%s-%s-%d", workflowName, tenantID, time.Now().Unix())
}

// Validate validates the temporal start workflow options
func (o *TemporalStartWorkflowOptions) Validate() error {
	if o.ExecutionTimeout <= 0 {
		return fmt.Errorf("execution timeout must be positive - provide a valid timeout duration")
	}
	if o.RetryPolicy != nil {
		if err := o.RetryPolicy.Validate(); err != nil {
			return fmt.Errorf("retry policy validation failed: %w", err)
		}
	}
	return nil
}
