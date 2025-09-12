package worker

import (
	"context"
	"sync"

	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/worker"
)

// temporalWorkerImpl implements TemporalWorker
type temporalWorkerImpl struct {
	worker    worker.Worker
	taskQueue types.TemporalTaskQueue
	started   bool
	mux       sync.RWMutex
	logger    *logger.Logger
}

// temporalWorkerManagerImpl implements TemporalWorkerManager
type temporalWorkerManagerImpl struct {
	client  client.TemporalClient
	logger  *logger.Logger
	workers map[types.TemporalTaskQueue]*temporalWorkerImpl
	mux     sync.RWMutex
}

// NewTemporalWorkerManager creates a new worker manager instance
func NewTemporalWorkerManager(client client.TemporalClient, logger *logger.Logger) TemporalWorkerManager {
	return &temporalWorkerManagerImpl{
		client:  client,
		logger:  logger,
		workers: make(map[types.TemporalTaskQueue]*temporalWorkerImpl),
	}
}

// GetOrCreateWorker implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) GetOrCreateWorker(taskQueue types.TemporalTaskQueue, options *models.WorkerOptions) (TemporalWorker, error) {
	if err := taskQueue.Validate(); err != nil {
		return nil, errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}

	wm.mux.Lock()
	defer wm.mux.Unlock()

	if w, exists := wm.workers[taskQueue]; exists {
		return w, nil
	}

	// Create new worker with options
	w := worker.New(wm.client.GetRawClient(), taskQueue.String(), options.ToSDKOptions())
	workerInstance := &temporalWorkerImpl{
		worker:    w,
		taskQueue: taskQueue,
		logger:    wm.logger,
	}

	wm.workers[taskQueue] = workerInstance
	wm.logger.Info("Created worker", "task_queue", taskQueue.String())
	return workerInstance, nil
}

// StartWorker implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) StartWorker(taskQueue types.TemporalTaskQueue) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}

	wm.mux.RLock()
	w, exists := wm.workers[taskQueue]
	wm.mux.RUnlock()

	if !exists {
		return errors.NewError("worker not found for task queue").
			WithHintf("No worker exists for task queue: %s", taskQueue.String()).
			Mark(errors.ErrNotFound)
	}

	return w.Start(context.Background())
}

// StopWorker implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) StopWorker(taskQueue types.TemporalTaskQueue) error {
	if err := taskQueue.Validate(); err != nil {
		return errors.WithError(err).
			WithHint("Invalid task queue provided").
			Mark(errors.ErrValidation)
	}

	wm.mux.Lock()
	defer wm.mux.Unlock()

	if w, exists := wm.workers[taskQueue]; exists {
		if err := w.Stop(context.Background()); err != nil {
			return err
		}
		delete(wm.workers, taskQueue)
		wm.logger.Info("Stopped worker", "task_queue", taskQueue.String())
	}
	return nil
}

// StopAllWorkers implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) StopAllWorkers() error {
	wm.mux.Lock()
	defer wm.mux.Unlock()

	var lastErr error
	for taskQueue, w := range wm.workers {
		if err := w.Stop(context.Background()); err != nil {
			lastErr = err
			wm.logger.Error("Failed to stop worker", "task_queue", taskQueue.String(), "error", err)
		}
		wm.logger.Info("Stopped worker", "task_queue", taskQueue.String())
	}
	wm.workers = make(map[types.TemporalTaskQueue]*temporalWorkerImpl)
	return lastErr
}

// GetWorkerStatus implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) GetWorkerStatus() map[types.TemporalTaskQueue]bool {
	wm.mux.RLock()
	defer wm.mux.RUnlock()

	status := make(map[types.TemporalTaskQueue]bool)
	for taskQueue, worker := range wm.workers {
		status[taskQueue] = worker.IsStarted()
	}
	return status
}

// Start implements TemporalWorker
func (w *temporalWorkerImpl) Start(ctx context.Context) error {
	w.mux.Lock()
	defer w.mux.Unlock()

	if w.started {
		return nil
	}

	// Start the worker in background and immediately return
	if err := w.worker.Start(); err != nil {
		w.logger.Error("Worker failed", "task_queue", w.taskQueue.String(), "error", err)
	}

	w.started = true
	w.logger.Info("Worker started", "task_queue", w.taskQueue.String())
	return nil
}

// Stop implements TemporalWorker
func (w *temporalWorkerImpl) Stop(ctx context.Context) error {
	w.mux.Lock()
	defer w.mux.Unlock()

	if !w.started {
		return nil
	}

	w.worker.Stop()
	w.started = false
	w.logger.Info("Stopped worker", "task_queue", w.taskQueue.String())
	return nil
}

// IsStarted implements TemporalWorker
func (w *temporalWorkerImpl) IsStarted() bool {
	w.mux.RLock()
	defer w.mux.RUnlock()
	return w.started
}

// RegisterWorkflow implements TemporalWorker
func (w *temporalWorkerImpl) RegisterWorkflow(workflow interface{}) error {
	if workflow == nil {
		return errors.NewError("workflow is required").
			WithHint("Workflow parameter cannot be nil").
			Mark(errors.ErrValidation)
	}
	w.worker.RegisterWorkflow(workflow)
	w.logger.Info("Registered workflow", "task_queue", w.taskQueue.String())
	return nil
}

// RegisterActivity implements TemporalWorker
func (w *temporalWorkerImpl) RegisterActivity(activity interface{}) error {
	if activity == nil {
		return errors.NewError("activity is required").
			WithHint("Activity parameter cannot be nil").
			Mark(errors.ErrValidation)
	}
	w.worker.RegisterActivity(activity)
	w.logger.Info("Registered activity", "task_queue", w.taskQueue.String())
	return nil
}
