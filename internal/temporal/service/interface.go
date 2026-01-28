package service

import (
	"context"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// TemporalService is the main entry point for temporal operations
type TemporalService interface {
	// Core operations
	Start(ctx context.Context) error
	Stop(ctx context.Context) error
	IsHealthy(ctx context.Context) bool

	// Workflow operations
	StartWorkflow(ctx context.Context, options models.StartWorkflowOptions, workflow types.TemporalWorkflowType, args ...interface{}) (models.WorkflowRun, error)
	SignalWorkflow(ctx context.Context, workflowID, runID, signalName string, arg interface{}) error
	QueryWorkflow(ctx context.Context, workflowID, runID, queryType string, args ...interface{}) (interface{}, error)
	CancelWorkflow(ctx context.Context, workflowID, runID string) error
	TerminateWorkflow(ctx context.Context, workflowID, runID, reason string, details ...interface{}) error

	// Activity operations
	CompleteActivity(ctx context.Context, taskToken []byte, result interface{}, err error) error
	RecordActivityHeartbeat(ctx context.Context, taskToken []byte, details ...interface{}) error

	// Worker operations
	RegisterWorkflow(taskQueue types.TemporalTaskQueue, workflow interface{}) error
	RegisterActivity(taskQueue types.TemporalTaskQueue, activity interface{}) error
	StartWorker(taskQueue types.TemporalTaskQueue) error
	StopWorker(taskQueue types.TemporalTaskQueue) error
	StopAllWorkers() error

	// Utility operations
	GetWorkflowHistory(ctx context.Context, workflowID, runID string) (interface{}, error)
	DescribeWorkflowExecution(ctx context.Context, workflowID, runID string) (interface{}, error)

	// Unified workflow execution - handles everything internally
	ExecuteWorkflow(ctx context.Context, workflowType types.TemporalWorkflowType, params interface{}) (models.WorkflowRun, error)
	// Synchronous workflow execution - waits for workflow completion and returns result
	ExecuteWorkflowSync(ctx context.Context, workflowType types.TemporalWorkflowType, params interface{}, timeout int) (interface{}, error)
}
