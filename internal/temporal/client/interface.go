package client

import (
	"context"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
)

// TemporalClient is the interface for interacting with Temporal service
type TemporalClient interface {
	// Core client operations
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsHealthy(ctx context.Context) bool

	// Workflow operations
	StartWorkflow(ctx context.Context, options models.StartWorkflowOptions, workflow interface{}, args ...interface{}) (models.WorkflowRun, error)
	SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error
	QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (interface{}, error)
	CancelWorkflow(ctx context.Context, workflowID, runID string) error
	TerminateWorkflow(ctx context.Context, workflowID, runID, reason string, details ...interface{}) error

	// Activity operations
	CompleteActivity(ctx context.Context, taskToken []byte, result interface{}, err error) error
	RecordActivityHeartbeat(ctx context.Context, taskToken []byte, details ...interface{}) error

	// Utility operations
	GetWorkflowHistory(ctx context.Context, workflowID, runID string) (client.HistoryEventIterator, error)
	DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (*workflowservice.DescribeWorkflowExecutionResponse, error)

	// Raw client access (for advanced use cases)
	GetRawClient() client.Client
}
