package worker

import (
	"context"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
)

// TemporalWorker represents a single worker instance
type TemporalWorker interface {
	// Start starts the worker
	Start(ctx context.Context) error
	// Stop stops the worker
	Stop(ctx context.Context) error
	// IsStarted returns whether the worker is started
	IsStarted() bool
	// RegisterWorkflow registers a workflow with this worker
	RegisterWorkflow(workflow interface{}) error
	// RegisterActivity registers an activity with this worker
	RegisterActivity(activity interface{}) error
}

// TemporalWorkerManager manages multiple workers
type TemporalWorkerManager interface {
	// GetOrCreateWorker gets an existing worker or creates a new one
	GetOrCreateWorker(taskQueue types.TemporalTaskQueue, options *models.WorkerOptions) (TemporalWorker, error)
	// StartWorker starts a worker for the given task queue
	StartWorker(taskQueue types.TemporalTaskQueue) error
	// StopWorker stops a worker for the given task queue
	StopWorker(taskQueue types.TemporalTaskQueue) error
	// StopAllWorkers stops all workers
	StopAllWorkers() error
	// GetWorkerStatus returns the status of all workers
	GetWorkerStatus() map[types.TemporalTaskQueue]bool
}
