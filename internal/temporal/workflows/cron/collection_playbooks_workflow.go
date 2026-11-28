package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityRunCollectionPlaybooks = "RunCollectionPlaybooksActivity"
)

// CollectionPlaybooksWorkflow runs the collection playbooks of every environment with a live one: stops runs whose
// invoice was paid or voided, enrols matching invoices and runs due steps. Triggered daily by a Temporal schedule.
// Safe to retry: each step runs at most once per invoice.
func CollectionPlaybooksWorkflow(ctx workflow.Context, _ cronModels.CollectionPlaybooksWorkflowInput) error {
	log := workflow.GetLogger(ctx)

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    5 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	if err := workflow.ExecuteActivity(ctx, ActivityRunCollectionPlaybooks).Get(ctx, nil); err != nil {
		log.Error("CollectionPlaybooksWorkflow activity failed", "error", err)
		return err
	}

	log.Info("CollectionPlaybooksWorkflow completed")
	return nil
}
