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

// TemporalWorkerManager manages workers for different task queues
type TemporalWorkerManager struct {
	client  *temporal.TemporalClient
	logger  *logger.Logger
	workers map[string]*TemporalWorker
	mux     sync.RWMutex
}

// TemporalWorker wraps a temporal worker
type TemporalWorker struct {
	worker    worker.Worker
	taskQueue string
	started   bool
	mux       sync.RWMutex
	logger    *logger.Logger
}

// NewTemporalWorkerManager creates a new worker manager instance
func NewTemporalWorkerManager(client *temporal.TemporalClient, logger *logger.Logger) *TemporalWorkerManager {
	return &TemporalWorkerManager{
		client:  client,
		logger:  logger,
		workers: make(map[string]*TemporalWorker),
	}
}

// getOrCreateWorker gets an existing worker or creates a new one
func (wm *TemporalWorkerManager) getOrCreateWorker(taskQueue string) (*TemporalWorker, error) {
	if taskQueue == "" {
		return nil, fmt.Errorf("task queue is required")
	}

	wm.mux.Lock()
	defer wm.mux.Unlock()

	if w, exists := wm.workers[taskQueue]; exists {
		return w, nil
	}

	w := worker.New(wm.client.Client, taskQueue, worker.Options{})
	workerInstance := &TemporalWorker{
		worker:    w,
		taskQueue: taskQueue,
		logger:    wm.logger,
	}

	wm.workers[taskQueue] = workerInstance
	wm.logger.Info("Created worker", "task_queue", taskQueue)
	return workerInstance, nil
}

// StartWorker starts a worker for the given task queue
func (wm *TemporalWorkerManager) StartWorker(taskQueue string) error {
	if taskQueue == "" {
		return fmt.Errorf("task queue is required")
	}
	w, err := wm.getOrCreateWorker(taskQueue)
	if err != nil {
		return err
	}
	return w.Start()
}

// StopWorker stops a worker for the given task queue
func (wm *TemporalWorkerManager) StopWorker(taskQueue string) error {
	wm.mux.Lock()
	defer wm.mux.Unlock()

	if w, exists := wm.workers[taskQueue]; exists {
		w.Stop()
		delete(wm.workers, taskQueue)
		wm.logger.Info("Stopped worker", "task_queue", taskQueue)
	}
	return nil
}

// StopAllWorkers stops all workers
func (wm *TemporalWorkerManager) StopAllWorkers() error {
	wm.mux.Lock()
	defer wm.mux.Unlock()

	for taskQueue, w := range wm.workers {
		w.Stop()
		wm.logger.Info("Stopped worker", "task_queue", taskQueue)
	}
	wm.workers = make(map[string]*TemporalWorker)
	return nil
}

// RegisterWorkflow registers a workflow with a specific task queue worker
func (wm *TemporalWorkerManager) RegisterWorkflow(taskQueue string, workflow interface{}) error {
	if taskQueue == "" {
		return fmt.Errorf("task queue is required")
	}
	if workflow == nil {
		return fmt.Errorf("workflow function is required")
	}
	w, err := wm.getOrCreateWorker(taskQueue)
	if err != nil {
		return err
	}
	w.worker.RegisterWorkflow(workflow)
	wm.logger.Info("Registered workflow", "task_queue", taskQueue)
	return nil
}

// RegisterActivity registers an activity with a specific task queue worker
func (wm *TemporalWorkerManager) RegisterActivity(taskQueue string, activity interface{}) error {
	if taskQueue == "" {
		return fmt.Errorf("task queue is required")
	}
	if activity == nil {
		return fmt.Errorf("activity function is required")
	}
	w, err := wm.getOrCreateWorker(taskQueue)
	if err != nil {
		return err
	}
	w.worker.RegisterActivity(activity)
	wm.logger.Info("Registered activity", "task_queue", taskQueue)
	return nil
}

// GetWorkerStatus returns the status of all workers
func (wm *TemporalWorkerManager) GetWorkerStatus() map[string]bool {
	wm.mux.RLock()
	defer wm.mux.RUnlock()

	status := make(map[string]bool)
	for taskQueue, worker := range wm.workers {
		status[taskQueue] = worker.IsStarted()
	}
	return status
}

// Start starts the worker
func (w *TemporalWorker) Start() error {
	w.mux.Lock()
	defer w.mux.Unlock()

	if w.started {
		return nil
	}

	go func() {
		if err := w.worker.Start(); err != nil {
			w.logger.Errorf("Failed to start worker: %v", err)
			// Worker failed - this is expected during shutdown
		}
	}()

	w.started = true
	return nil
}

// Stop stops the worker
func (w *TemporalWorker) Stop() {
	w.mux.Lock()
	defer w.mux.Unlock()

	if w.started {
		w.worker.Stop()
		w.started = false
	}
}

// IsStarted returns whether the worker is started
func (w *TemporalWorker) IsStarted() bool {
	w.mux.RLock()
	defer w.mux.RUnlock()
	return w.started
}

// RegisterWithLifecycle registers the worker manager with fx lifecycle
func (wm *TemporalWorkerManager) RegisterWithLifecycle(lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			wm.logger.Info("Temporal worker manager started")
			return nil
		},
		OnStop: func(ctx context.Context) error {
			wm.logger.Info("Temporal worker manager stopping")
			return wm.StopAllWorkers()
		},
	})
}
