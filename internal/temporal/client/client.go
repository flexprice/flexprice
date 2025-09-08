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

// ExecuteTemporalWorkflow executes a workflow with default options
func ExecuteTemporalWorkflow(c *temporal.TemporalClient, ctx context.Context, workflowName string, input interface{}) (client.WorkflowRun, error) {
	return ExecuteTemporalWorkflowWithOptions(c, ctx, workflowName, input, DefaultTemporalStartWorkflowOptions())
}

// ExecuteTemporalWorkflowWithOptions executes a workflow with custom options
func ExecuteTemporalWorkflowWithOptions(c *temporal.TemporalClient, ctx context.Context, workflowName string, input interface{}, options *TemporalStartWorkflowOptions) (client.WorkflowRun, error) {
	if options == nil {
		options = DefaultTemporalStartWorkflowOptions()
	}

	// Validate workflow name
	if workflowName == "" {
		return nil, fmt.Errorf("workflow name cannot be empty - provide a valid workflow name")
	}

	// Validate input
	if input == nil {
		return nil, fmt.Errorf("workflow input cannot be nil - provide valid input data")
	}

	// Generate workflow ID if not provided
	workflowID := generateTemporalWorkflowID(workflowName, ctx)

	// Validate required fields
	if options.ExecutionTimeout <= 0 {
		options.ExecutionTimeout = time.Hour // Default timeout
	}

	temporalOptions := &client.StartWorkflowOptions{
		ID:                 workflowID,
		WorkflowRunTimeout: options.ExecutionTimeout,
	}

	// Add retry policy if provided
	if options.RetryPolicy != nil {
		if err := options.RetryPolicy.Validate(); err != nil {
			return nil, fmt.Errorf("invalid retry policy: %w - check retry policy configuration", err)
		}
		temporalOptions.RetryPolicy = &temporalsdk.RetryPolicy{
			InitialInterval:        options.RetryPolicy.InitialInterval,
			BackoffCoefficient:     options.RetryPolicy.BackoffCoefficient,
			MaximumInterval:        options.RetryPolicy.MaximumInterval,
			MaximumAttempts:        options.RetryPolicy.MaximumAttempts,
			NonRetryableErrorTypes: options.RetryPolicy.NonRetryableErrorTypes,
		}
	}

	run, err := c.Client.ExecuteWorkflow(ctx, *temporalOptions, workflowName, input)
	if err != nil {
		return nil, fmt.Errorf("failed to execute temporal workflow '%s' with ID '%s': %w - check temporal server connectivity and workflow registration", workflowName, workflowID, err)
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
