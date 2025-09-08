package client

import (
	"context"
	"crypto/tls"
	"sync"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

// temporalClientImpl implements the TemporalClient interface
type temporalClientImpl struct {
	client     client.Client
	logger     *logger.Logger
	isStarted  bool
	startMutex sync.Mutex
}

// temporalClientFactory implements the TemporalClientFactory interface
type temporalClientFactory struct {
	logger *logger.Logger
}

// NewTemporalClientFactory creates a new factory for temporal clients
func NewTemporalClientFactory(logger *logger.Logger) TemporalClientFactory {
	return &temporalClientFactory{
		logger: logger,
	}
}

// CreateClient implements TemporalClientFactory
func (f *temporalClientFactory) CreateClient(options *models.ClientOptions) (TemporalClient, error) {
	f.logger.Info("Creating new temporal client", "namespace", options.Namespace)

	// Convert our options to SDK options
	sdkOptions := client.Options{
		HostPort:      options.Address,
		Namespace:     options.Namespace,
		DataConverter: options.DataConverter,
		HeadersProvider: &models.APIKeyProvider{
			APIKey:    options.APIKey,
			Namespace: options.Namespace,
		},
	}

	// Configure TLS if enabled
	if options.TLS {
		sdkOptions.ConnectionOptions.TLS = &tls.Config{}
	}

	// Create the temporal client
	c, err := client.Dial(sdkOptions)
	if err != nil {
		f.logger.Error("Failed to create temporal client", "error", err)
		return nil, err
	}

	return &temporalClientImpl{
		client: c,
		logger: f.logger,
	}, nil
}

// Start implements TemporalClient
func (c *temporalClientImpl) Start(ctx context.Context) error {
	c.startMutex.Lock()
	defer c.startMutex.Unlock()

	if c.isStarted {
		return nil
	}

	// Check health to ensure connection is working
	if _, err := c.client.CheckHealth(ctx, &client.CheckHealthRequest{}); err != nil {
		c.logger.Error("Failed to check client health during start", "error", err)
		return err
	}

	c.isStarted = true
	c.logger.Info("Temporal client started successfully")
	return nil
}

// Stop implements TemporalClient
func (c *temporalClientImpl) Stop(ctx context.Context) error {
	c.startMutex.Lock()
	defer c.startMutex.Unlock()

	if !c.isStarted {
		return nil
	}

	c.client.Close()
	c.isStarted = false
	c.logger.Info("Temporal client stopped successfully")
	return nil
}

// IsHealthy implements TemporalClient
func (c *temporalClientImpl) IsHealthy(ctx context.Context) bool {
	_, err := c.client.CheckHealth(ctx, &client.CheckHealthRequest{})
	return err == nil
}

// StartWorkflow implements TemporalClient
func (c *temporalClientImpl) StartWorkflow(ctx context.Context, options models.StartWorkflowOptions, workflow interface{}, args ...interface{}) (models.WorkflowRun, error) {
	run, err := c.client.ExecuteWorkflow(ctx, options.ToSDKOptions(), workflow, args...)
	if err != nil {
		return nil, err
	}
	return models.NewWorkflowRun(run), nil
}

// SignalWorkflow implements TemporalClient
func (c *temporalClientImpl) SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error {
	return c.client.SignalWorkflow(ctx, workflowID, runID, signalName, arg)
}

// QueryWorkflow implements TemporalClient
func (c *temporalClientImpl) QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (interface{}, error) {
	response, err := c.client.QueryWorkflow(ctx, workflowID, runID, queryType, args...)
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := response.Get(&result); err != nil {
		return nil, err
	}
	return result, nil
}

// CancelWorkflow implements TemporalClient
func (c *temporalClientImpl) CancelWorkflow(ctx context.Context, workflowID, runID string) error {
	return c.client.CancelWorkflow(ctx, workflowID, runID)
}

// TerminateWorkflow implements TemporalClient
func (c *temporalClientImpl) TerminateWorkflow(ctx context.Context, workflowID, runID, reason string, details ...interface{}) error {
	return c.client.TerminateWorkflow(ctx, workflowID, runID, reason, details...)
}

// CompleteActivity implements TemporalClient
func (c *temporalClientImpl) CompleteActivity(ctx context.Context, taskToken []byte, result interface{}, err error) error {
	return c.client.CompleteActivity(ctx, taskToken, result, err)
}

// RecordActivityHeartbeat implements TemporalClient
func (c *temporalClientImpl) RecordActivityHeartbeat(ctx context.Context, taskToken []byte, details ...interface{}) error {
	return c.client.RecordActivityHeartbeat(ctx, taskToken, details...)
}

// GetWorkflowHistory implements TemporalClient
func (c *temporalClientImpl) GetWorkflowHistory(ctx context.Context, workflowID, runID string) (client.HistoryEventIterator, error) {
	iter := c.client.GetWorkflowHistory(ctx, workflowID, runID, true, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	return iter, nil
}

// DescribeWorkflowExecution implements TemporalClient
func (c *temporalClientImpl) DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return c.client.DescribeWorkflowExecution(ctx, workflowID, runID)
}

// GetRawClient implements TemporalClient
func (c *temporalClientImpl) GetRawClient() client.Client {
	return c.client
}
