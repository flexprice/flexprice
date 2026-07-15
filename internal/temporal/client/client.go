package client

import (
	"context"
	"crypto/tls"
	"errors"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const temporalDialTimeout = 2 * time.Minute

type temporalDialFunc func(context.Context, client.Options) (client.Client, error)

// APIKeyProvider provides headers for API key authentication
type APIKeyProvider struct {
	APIKey    string
	Namespace string
}

// GetHeaders implements client.HeadersProvider using existing constants
func (a *APIKeyProvider) GetHeaders(_ context.Context) (map[string]string, error) {
	return map[string]string{
		types.HeaderAuthorization: "Bearer " + a.APIKey,
		"temporal-namespace":      a.Namespace,
	}, nil
}

// temporalClient implements the TemporalClient interface
type temporalClient struct {
	client     client.Client
	logger     *logger.Logger
	isStarted  bool
	startMutex sync.Mutex
}

// NewTemporalClient creates a new temporal client instance
func NewTemporalClient(options *models.ClientOptions, logger *logger.Logger) (TemporalClient, error) {
	logger.Info(context.Background(), "Creating new temporal client", "namespace", options.Namespace)

	// Convert our options to SDK options
	sdkOptions := client.Options{
		HostPort:      options.Address,
		Namespace:     options.Namespace,
		DataConverter: options.DataConverter,
		HeadersProvider: &APIKeyProvider{
			APIKey:    options.APIKey,
			Namespace: options.Namespace,
		},
	}

	// Configure TLS if enabled
	if options.TLS {
		sdkOptions.ConnectionOptions.TLS = &tls.Config{
			MinVersion: tls.VersionTLS12,
			// Use system's root CA certificates for verification
			// ServerName will be automatically set from the connection address
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), temporalDialTimeout)
	defer cancel()

	retry := backoff.NewExponentialBackOff()
	retry.InitialInterval = time.Second
	retry.MaxInterval = 5 * time.Second
	retry.MaxElapsedTime = temporalDialTimeout

	c, err := dialTemporalClient(ctx, sdkOptions, client.DialContext, retry)
	if err != nil {
		logger.Error(context.Background(), "Failed to create temporal client", "error", err)
		return nil, err
	}

	return &temporalClient{
		client: c,
		logger: logger,
	}, nil
}

func dialTemporalClient(ctx context.Context, options client.Options, dial temporalDialFunc, retry backoff.BackOff) (client.Client, error) {
	var temporalClient client.Client
	err := backoff.Retry(func() error {
		var err error
		temporalClient, err = dial(ctx, options)
		if err != nil && !isRetryableTemporalDialError(err) {
			return backoff.Permanent(err)
		}
		return err
	}, backoff.WithContext(retry, ctx))
	return temporalClient, err
}

func isRetryableTemporalDialError(err error) bool {
	var unavailable *serviceerror.Unavailable
	var deadlineExceeded *serviceerror.DeadlineExceeded
	return errors.As(err, &unavailable) ||
		errors.As(err, &deadlineExceeded) ||
		status.Code(err) == codes.Unavailable ||
		status.Code(err) == codes.DeadlineExceeded
}

// Start implements TemporalClient
func (c *temporalClient) Start(ctx context.Context) error {
	c.startMutex.Lock()
	defer c.startMutex.Unlock()

	if c.isStarted {
		return nil
	}

	// Check health to ensure connection is working
	if _, err := c.client.CheckHealth(ctx, &client.CheckHealthRequest{}); err != nil {
		c.logger.Error(ctx, "Failed to check client health during start", "error", err)
		return err
	}

	c.isStarted = true
	c.logger.Info(ctx, "Temporal client started")
	return nil
}

// Stop implements TemporalClient
func (c *temporalClient) Stop(ctx context.Context) error {
	c.startMutex.Lock()
	defer c.startMutex.Unlock()

	if !c.isStarted {
		return nil
	}

	c.client.Close()
	c.isStarted = false
	c.logger.Info(ctx, "Temporal client stopped successfully")
	return nil
}

// IsHealthy implements TemporalClient
func (c *temporalClient) IsHealthy(ctx context.Context) bool {
	_, err := c.client.CheckHealth(ctx, &client.CheckHealthRequest{})
	return err == nil
}

// StartWorkflow implements TemporalClient
func (c *temporalClient) StartWorkflow(ctx context.Context, options models.StartWorkflowOptions, workflow interface{}, args ...interface{}) (models.WorkflowRun, error) {
	run, err := c.client.ExecuteWorkflow(ctx, options.ToSDKOptions(), workflow, args...)
	if err != nil {
		return nil, err
	}
	return models.NewWorkflowRun(run), nil
}

// SignalWorkflow implements TemporalClient
func (c *temporalClient) SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error {
	return c.client.SignalWorkflow(ctx, workflowID, runID, signalName, arg)
}

// QueryWorkflow implements TemporalClient
func (c *temporalClient) QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (interface{}, error) {
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
func (c *temporalClient) CancelWorkflow(ctx context.Context, workflowID, runID string) error {
	return c.client.CancelWorkflow(ctx, workflowID, runID)
}

// TerminateWorkflow implements TemporalClient
func (c *temporalClient) TerminateWorkflow(ctx context.Context, workflowID, runID, reason string, details ...interface{}) error {
	return c.client.TerminateWorkflow(ctx, workflowID, runID, reason, details...)
}

// CompleteActivity implements TemporalClient
func (c *temporalClient) CompleteActivity(ctx context.Context, taskToken []byte, result interface{}, err error) error {
	return c.client.CompleteActivity(ctx, taskToken, result, err)
}

// RecordActivityHeartbeat implements TemporalClient
func (c *temporalClient) RecordActivityHeartbeat(ctx context.Context, taskToken []byte, details ...interface{}) error {
	return c.client.RecordActivityHeartbeat(ctx, taskToken, details...)
}

// GetWorkflowHistory implements TemporalClient
func (c *temporalClient) GetWorkflowHistory(ctx context.Context, workflowID, runID string) (client.HistoryEventIterator, error) {
	iter := c.client.GetWorkflowHistory(ctx, workflowID, runID, true, enums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	return iter, nil
}

// DescribeWorkflowExecution implements TemporalClient
func (c *temporalClient) DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error) {
	return c.client.DescribeWorkflowExecution(ctx, workflowID, runID)
}

// CreateSchedule implements TemporalClient
func (c *temporalClient) CreateSchedule(ctx context.Context, options models.CreateScheduleOptions) (models.ScheduleHandle, error) {
	schedule, err := c.client.ScheduleClient().Create(ctx, options.ToSDKOptions())
	if err != nil {
		return nil, err
	}
	return models.NewScheduleHandle(schedule), nil
}

// GetScheduleHandle implements TemporalClient
func (c *temporalClient) GetScheduleHandle(ctx context.Context, scheduleID string) models.ScheduleHandle {
	handle := c.client.ScheduleClient().GetHandle(ctx, scheduleID)
	return models.NewScheduleHandle(handle)
}

// GetRawClient implements TemporalClient
func (c *temporalClient) GetRawClient() client.Client {
	return c.client
}
