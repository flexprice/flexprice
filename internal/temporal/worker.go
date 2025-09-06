package temporal

import (
	"context"

	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/service"
	"go.temporal.io/sdk/worker"
	"go.uber.org/fx"
)

// Worker manages the Temporal worker instance.
type Worker struct {
	worker worker.Worker
	log    *logger.Logger
}

// NewWorker creates a new Temporal worker and registers workflows and activities.
func NewWorker(client *TemporalClient, cfg config.TemporalConfig, params service.ServiceParams) *Worker {
	w := worker.New(client.Client, cfg.TaskQueue, worker.Options{})

	RegisterWorkflowsAndActivities(w, params)

	return &Worker{
		worker: w,
		log:    params.Logger,
	}
}

// Start starts the Temporal worker.
func (w *Worker) Start() error {
	w.log.Info("Starting temporal worker...")
	return w.worker.Start()
}

// Stop stops the Temporal worker.
func (w *Worker) Stop() {
	w.log.Info("Stopping temporal worker...")
	if w.worker != nil {
		w.worker.Stop()
	}
}

// RegisterWithLifecycle registers the worker with the fx lifecycle.
func (w *Worker) RegisterWithLifecycle(lc fx.Lifecycle) {
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return w.Start()
		},
		OnStop: func(ctx context.Context) error {
			done := make(chan struct{})
			go func() {
				w.Stop()
				close(done)
			}()

			select {
			case <-done:
				w.log.Info("Temporal worker stopped successfully")
			case <-ctx.Done():
				w.log.Error("Timeout while stopping temporal worker")
			}
			return nil
		},
	})
}
