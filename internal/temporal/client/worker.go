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
)

// WorkerManager manages global workers for different task queues
type WorkerManager struct {
	client  *temporal.TemporalClient
	logger  *logger.Logger
	workers map[string]*Worker
	mux     sync.RWMutex
}

// Worker wraps a temporal worker with additional functionality
type Worker struct {
	worker    worker.Worker
	logger    *logger.Logger
	taskQueue string
	started   bool
	mux       sync.RWMutex
}

// GetWorkerManager returns the global worker manager instance
func GetWorkerManager() *WorkerManager {
	return globalWorkerManager
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
	if taskQueue == "" {
		return nil, fmt.Errorf("task queue is required")
	}

	// Check if worker exists
	wm.mux.RLock()
	if w, exists := wm.workers[taskQueue]; exists {
		wm.mux.RUnlock()
		return w, nil
	}
	wm.mux.RUnlock()

	// Create new worker
	wm.mux.Lock()
	defer wm.mux.Unlock()

	// Double-check after acquiring write lock
	if w, exists := wm.workers[taskQueue]; exists {
		return w, nil
	}

	// Create worker with simple options
	w := worker.New(wm.client.Client, taskQueue, worker.Options{})
	workerInstance := &Worker{
		worker:    w,
		logger:    wm.logger,
		taskQueue: taskQueue,
		started:   false,
	}

	wm.workers[taskQueue] = workerInstance
	wm.logger.Info("Created worker for task queue", "task_queue", taskQueue)
	return workerInstance, nil
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
func (wm *WorkerManager) StopWorker(taskQueue string) error {
	wm.mux.Lock()
	defer wm.mux.Unlock()

	if worker, exists := wm.workers[taskQueue]; exists {
		_ = worker.Stop() // Ignore error for simplicity
		delete(wm.workers, taskQueue)
		wm.logger.Info("Stopped worker", "task_queue", taskQueue)
	}
	return nil
}

// StopAllWorkers stops all workers
func (wm *WorkerManager) StopAllWorkers() error {
	wm.mux.Lock()
	defer wm.mux.Unlock()

	for taskQueue, worker := range wm.workers {
		_ = worker.Stop() // Ignore error for simplicity
		wm.logger.Info("Stopped worker", "task_queue", taskQueue)
	}
	wm.workers = make(map[string]*Worker)
	return nil
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

// GetWorkerStatus returns the status of all workers
func (wm *WorkerManager) GetWorkerStatus() map[string]bool {
	wm.mux.RLock()
	defer wm.mux.RUnlock()

	status := make(map[string]bool)
	for taskQueue, worker := range wm.workers {
		status[taskQueue] = worker.IsStarted()
	}
	return status
}

// Start starts the worker
func (w *Worker) Start() error {
	w.mux.Lock()
	defer w.mux.Unlock()

	if w.started {
		return nil
	}

	w.logger.Info("Starting worker", "task_queue", w.taskQueue)

	// Start worker in goroutine
	go func() {
		if err := w.worker.Start(); err != nil {
			w.logger.Error("Worker failed", "task_queue", w.taskQueue, "error", err)
		}
	}()

	w.started = true
	return nil
}

// Stop stops the worker
func (w *Worker) Stop() error {
	w.mux.Lock()
	defer w.mux.Unlock()

	if !w.started {
		return nil
	}

	w.logger.Info("Stopping worker", "task_queue", w.taskQueue)
	w.worker.Stop()
	w.started = false
	return nil
}

// IsStarted returns whether the worker is started
func (w *Worker) IsStarted() bool {
	w.mux.RLock()
	defer w.mux.RUnlock()
	return w.started
}

// RegisterWorkflow registers a workflow with the worker
func (w *Worker) RegisterWorkflow(workflow interface{}) {
	w.worker.RegisterWorkflow(workflow)
}

// RegisterActivity registers an activity with the worker
func (w *Worker) RegisterActivity(activity interface{}) {
	w.worker.RegisterActivity(activity)
}

// RegisterWithLifecycle registers the worker manager with fx lifecycle
func (wm *WorkerManager) RegisterWithLifecycle(lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			wm.logger.Info("Worker manager started")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			wm.logger.Info("Worker manager stopping")
			return wm.StopAllWorkers()
		},
	})
}
