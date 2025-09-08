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

// ExecuteWorkflow executes a workflow with default options
func ExecuteWorkflow(c *temporal.TemporalClient, ctx context.Context, workflowName string, input interface{}) (client.WorkflowRun, error) {
	return ExecuteWorkflowWithOptions(c, ctx, workflowName, input, DefaultStartWorkflowOptions())
}

// ExecuteWorkflowWithOptions executes a workflow with custom options
func ExecuteWorkflowWithOptions(c *temporal.TemporalClient, ctx context.Context, workflowName string, input interface{}, options *StartWorkflowOptions) (client.WorkflowRun, error) {
	if options == nil {
		options = DefaultStartWorkflowOptions()
	}

	// Generate workflow ID if not provided
	if options.ID == "" {
		options.ID = generateWorkflowID(workflowName, ctx)
	}

	// Validate required fields
	if options.TaskQueue == "" {
		return nil, fmt.Errorf("task queue is required for workflow execution")
	}

	temporalOptions := &client.StartWorkflowOptions{
		ID:                 options.ID,
		TaskQueue:          options.TaskQueue,
		WorkflowRunTimeout: options.ExecutionTimeout,
	}

	// Add retry policy if provided
	if options.RetryPolicy != nil {
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
		return nil, fmt.Errorf("failed to execute workflow %s: %w", workflowName, err)
	}

	return run, nil
}

// generateWorkflowID generates a unique workflow ID
func generateWorkflowID(workflowName string, ctx context.Context) string {
	tenantID := types.GetTenantID(ctx)
	if tenantID == "" {
		tenantID = "unknown"
	}
	return fmt.Sprintf("%s-%s-%d", workflowName, tenantID, time.Now().Unix())
}

// StartWorkflowOptions represents options for workflow execution
type StartWorkflowOptions struct {
	ID               string
	TaskQueue        string
	ExecutionTimeout time.Duration
	RetryPolicy      *RetryPolicy
}

// Validate validates the workflow options
func (o *StartWorkflowOptions) Validate() error {
	if o.TaskQueue == "" {
		return fmt.Errorf("task queue is required")
	}
	if o.ExecutionTimeout <= 0 {
		o.ExecutionTimeout = time.Hour // Default timeout
	}
	return nil
}
