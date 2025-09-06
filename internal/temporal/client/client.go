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
	if options.ID == "" {
		options.ID = fmt.Sprintf("%s-%s-%d", workflowName, types.GetTenantID(ctx), time.Now().Unix())
	}

	temporalOptions := &client.StartWorkflowOptions{
		ID:                 options.ID,
		TaskQueue:          options.TaskQueue,
		WorkflowRunTimeout: options.ExecutionTimeout,
	}

	run, err := c.Client.ExecuteWorkflow(ctx, *temporalOptions, workflowName, input)
	if err != nil {
		return nil, fmt.Errorf("failed to execute workflow %s: %w", workflowName, err)
	}

	return run, nil
}

// StartWorkflowOptions represents options for workflow execution
type StartWorkflowOptions struct {
	ID               string
	TaskQueue        string
	ExecutionTimeout time.Duration
}
