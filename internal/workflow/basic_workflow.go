package workflow

import (
	"go.temporal.io/sdk/worker"
)

// RegisterWorkflows registers all workflows with the worker
func RegisterWorkflows(w worker.Worker) {
	w.RegisterWorkflow(UpdateBillingPeriodsWorkflow)
}

// RegisterActivities registers all activities with the worker
func RegisterActivities(w worker.Worker) {
	w.RegisterActivity(UpdateBillingPeriodsActivity)
}
