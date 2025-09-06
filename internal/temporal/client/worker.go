package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/temporal"
	"go.temporal.io/sdk/worker"
	"go.uber.org/fx"
)

var (
	globalWorkerManager *WorkerManager
	workerManagerOnce   sync.Once
	workerManagerMux    sync.RWMutex
)

// WorkerManager manages global workers for different task queues
type WorkerManager struct {
	client     *temporal.TemporalClient
	logger     *logger.Logger
	workers    map[string]*Worker
	workersMux sync.RWMutex
}

// Worker wraps a temporal worker with additional functionality
type Worker struct {
	worker worker.Worker
	logger *logger.Logger
}

// GetWorkerManager returns the global worker manager instance
func GetWorkerManager() *WorkerManager {
	workerManagerMux.RLock()
	if globalWorkerManager != nil {
		defer workerManagerMux.RUnlock()
		return globalWorkerManager
	}
	workerManagerMux.RUnlock()
	return nil
}

// InitWorkerManager initializes the global worker manager
func InitWorkerManager(client *temporal.TemporalClient, logger *logger.Logger) *WorkerManager {
	workerManagerOnce.Do(func() {
		globalWorkerManager = &WorkerManager{
			client:  client,
			logger:  logger,
			workers: make(map[string]*Worker),
		}
	})
	return globalWorkerManager
}

// GetOrCreateWorker gets an existing worker or creates a new one for the task queue
func (wm *WorkerManager) GetOrCreateWorker(taskQueue string) (*Worker, error) {
	wm.workersMux.Lock()
	defer wm.workersMux.Unlock()

	// Return existing worker if found
	if w, exists := wm.workers[taskQueue]; exists {
		return w, nil
	}

	// Create new worker
	w := worker.New(wm.client.Client, taskQueue, worker.Options{})
	worker := &Worker{
		worker: w,
		logger: wm.logger,
	}

	wm.workers[taskQueue] = worker
	wm.logger.Info("Created new worker for task queue", "task_queue", taskQueue)
	return worker, nil
}

// StartWorker starts a worker for the given task queue
func (wm *WorkerManager) StartWorker(taskQueue string) error {
	worker, err := wm.GetOrCreateWorker(taskQueue)
	if err != nil {
		return err
	}

	return worker.Start()
}

// StopWorker stops a worker for the given task queue
func (wm *WorkerManager) StopWorker(taskQueue string) {
	wm.workersMux.Lock()
	defer wm.workersMux.Unlock()

	if worker, exists := wm.workers[taskQueue]; exists {
		worker.Stop()
		delete(wm.workers, taskQueue)
		wm.logger.Info("Stopped worker for task queue", "task_queue", taskQueue)
	}
}

// StopAllWorkers stops all workers
func (wm *WorkerManager) StopAllWorkers() {
	wm.workersMux.Lock()
	defer wm.workersMux.Unlock()

	for taskQueue, worker := range wm.workers {
		worker.Stop()
		wm.logger.Info("Stopped worker for task queue", "task_queue", taskQueue)
	}
	wm.workers = make(map[string]*Worker)
}

// RegisterWorkflow registers a workflow with a specific task queue worker
func (wm *WorkerManager) RegisterWorkflow(taskQueue string, workflow interface{}) error {
	worker, err := wm.GetOrCreateWorker(taskQueue)
	if err != nil {
		return err
	}

	worker.RegisterWorkflow(workflow)
	return nil
}

// RegisterActivity registers an activity with a specific task queue worker
func (wm *WorkerManager) RegisterActivity(taskQueue string, activity interface{}) error {
	worker, err := wm.GetOrCreateWorker(taskQueue)
	if err != nil {
		return err
	}

	worker.RegisterActivity(activity)
	return nil
}

// Start starts the worker
func (w *Worker) Start() error {
	w.logger.Info("Starting temporal worker")
	if err := w.worker.Start(); err != nil {
		return fmt.Errorf("failed to start worker: %w", err)
	}
	w.logger.Info("Temporal worker started successfully")
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() {
	w.logger.Info("Stopping temporal worker")
	if w.worker != nil {
		w.worker.Stop()
		w.logger.Info("Temporal worker stopped successfully")
	}
}

// RegisterWorkflow registers a workflow with the worker
func (w *Worker) RegisterWorkflow(workflow interface{}) {
	w.worker.RegisterWorkflow(workflow)
	w.logger.Debug("Registered workflow with worker")
}

// RegisterActivity registers an activity with the worker
func (w *Worker) RegisterActivity(activity interface{}) {
	w.worker.RegisterActivity(activity)
	w.logger.Debug("Registered activity with worker")
}

// RegisterWithLifecycle registers the worker manager with fx lifecycle
func (wm *WorkerManager) RegisterWithLifecycle(lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			wm.logger.Info("Starting temporal worker manager")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			wm.logger.Info("Stopping temporal worker manager")
			wm.StopAllWorkers()
			return nil
		},
	})
}
