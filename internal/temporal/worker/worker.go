package worker

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/worker"
)

// temporalWorkerImpl implements TemporalWorker
type temporalWorkerImpl struct {
	worker    worker.Worker
	taskQueue string
	started   bool
	mux       sync.RWMutex
	logger    *logger.Logger
}

// temporalWorkerManagerImpl implements TemporalWorkerManager
type temporalWorkerManagerImpl struct {
	client  client.TemporalClient
	logger  *logger.Logger
	workers map[string]*temporalWorkerImpl
	mux     sync.RWMutex
}

// NewTemporalWorkerManager creates a new worker manager instance
func NewTemporalWorkerManager(client client.TemporalClient, logger *logger.Logger) TemporalWorkerManager {
	return &temporalWorkerManagerImpl{
		client:  client,
		logger:  logger,
		workers: make(map[string]*temporalWorkerImpl),
	}
}

// GetOrCreateWorker implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) GetOrCreateWorker(taskQueue string, options *models.WorkerOptions) (TemporalWorker, error) {
	if taskQueue == "" {
		return nil, fmt.Errorf("task queue is required")
	}

	wm.mux.Lock()
	defer wm.mux.Unlock()

	if w, exists := wm.workers[taskQueue]; exists {
		return w, nil
	}

	// Create new worker with options
	w := worker.New(wm.client.GetRawClient(), taskQueue, options.ToSDKOptions())
	workerInstance := &temporalWorkerImpl{
		worker:    w,
		taskQueue: taskQueue,
		logger:    wm.logger,
	}

	wm.workers[taskQueue] = workerInstance
	wm.logger.Info("Created worker", "task_queue", taskQueue)
	return workerInstance, nil
}

// StartWorker implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) StartWorker(taskQueue string) error {
	if taskQueue == "" {
		return fmt.Errorf("task queue is required")
	}

	wm.mux.RLock()
	w, exists := wm.workers[taskQueue]
	wm.mux.RUnlock()

	if !exists {
		return fmt.Errorf("worker not found for task queue: %s", taskQueue)
	}

	return w.Start(context.Background())
}

// StopWorker implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) StopWorker(taskQueue string) error {
	wm.mux.Lock()
	defer wm.mux.Unlock()

	if w, exists := wm.workers[taskQueue]; exists {
		if err := w.Stop(context.Background()); err != nil {
			return err
		}
		delete(wm.workers, taskQueue)
		wm.logger.Info("Stopped worker", "task_queue", taskQueue)
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
			wm.logger.Error("Failed to stop worker", "task_queue", taskQueue, "error", err)
		}
		wm.logger.Info("Stopped worker", "task_queue", taskQueue)
	}
	wm.workers = make(map[string]*temporalWorkerImpl)
	return lastErr
}

// GetWorkerStatus implements TemporalWorkerManager
func (wm *temporalWorkerManagerImpl) GetWorkerStatus() map[string]bool {
	wm.mux.RLock()
	defer wm.mux.RUnlock()

	status := make(map[string]bool)
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

	go func() {
		if err := w.worker.Start(); err != nil {
			w.logger.Error("Failed to start worker", "task_queue", w.taskQueue, "error", err)
		}
	}()

	w.started = true
	w.logger.Info("Started worker", "task_queue", w.taskQueue)
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
	w.logger.Info("Stopped worker", "task_queue", w.taskQueue)
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
		return fmt.Errorf("workflow is required")
	}
	w.worker.RegisterWorkflow(workflow)
	w.logger.Info("Registered workflow", "task_queue", w.taskQueue)
	return nil
}

// RegisterActivity implements TemporalWorker
func (w *temporalWorkerImpl) RegisterActivity(activity interface{}) error {
	if activity == nil {
		return fmt.Errorf("activity is required")
	}
	w.worker.RegisterActivity(activity)
	w.logger.Info("Registered activity", "task_queue", w.taskQueue)
	return nil
}
