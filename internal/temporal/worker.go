package temporal

import (
	"github.com/flexprice/flexprice/internal/config"
	"go.temporal.io/sdk/worker"
)

// Worker manages the Temporal worker instance.
type Worker struct {
	worker worker.Worker
}

// NewWorker creates a new Temporal worker and registers workflows and activities.
func NewWorker(client *TemporalClient, cfg config.TemporalConfig) *Worker {
	w := worker.New(client.Client, cfg.TaskQueue, worker.Options{})

	// Use the existing registration function
	RegisterWorkflowsAndActivities(w)

	return &Worker{worker: w}
}

// Start starts the Temporal worker.
func (w *Worker) Start() error {
	return w.worker.Start()
}

// Stop stops the Temporal worker.
func (w *Worker) Stop() {
	if w.worker != nil {
		w.worker.Stop()
	}
}
