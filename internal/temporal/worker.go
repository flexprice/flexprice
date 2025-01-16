package temporal

import (
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/temporal/activities"
	"github.com/flexprice/flexprice/internal/temporal/workflows"
	"go.temporal.io/sdk/worker"
)

type Worker struct {
	worker worker.Worker
}

func NewWorker(
	c *TemporalClient,
	cfg config.TemporalConfig,
) *Worker {
	w := worker.New(c.Client, cfg.TaskQueue, worker.Options{})

	w.RegisterWorkflow(workflows.BillingWorkflow)
	w.RegisterActivity(&activities.BillingActivities{})

	return &Worker{worker: w}
}

func (w *Worker) Start() error {
	return w.worker.Start()
}

func (w *Worker) Stop() {
	w.worker.Stop()
}
