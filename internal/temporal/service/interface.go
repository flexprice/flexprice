package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/temporal/models"
)

// TemporalService is the main entry point for temporal operations
type TemporalService interface {
	// Core operations
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

	// Worker operations
	RegisterWorkflow(taskQueue string, workflow interface{}) error
	RegisterActivity(taskQueue string, activity interface{}) error
	StartWorker(taskQueue string) error
	StopWorker(taskQueue string) error
	StopAllWorkers() error

	// Utility operations
	GetWorkflowHistory(ctx context.Context, workflowID, runID string) (interface{}, error)
	DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (interface{}, error)
}
