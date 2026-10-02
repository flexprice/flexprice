package cron

import (
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	ActivityMaintainActivityPartitions = "MaintainPartitionsActivity"
	ActivityListArchivablePartitions   = "ListArchivablePartitionsActivity"
	ActivityExportActivityPartition    = "ExportPartitionActivity"
	ActivityDropActivityPartition      = "DropPartitionActivity"
)

// ActivityArchiveWorkflow creates upcoming activity_logs partitions, then archives
// partitions past the hot window to Parquet (gated by activity.archive.enabled). Runs daily.
func ActivityArchiveWorkflow(ctx workflow.Context, _ cronModels.ActivityArchiveWorkflowInput) (*cronModels.ActivityArchiveWorkflowResult, error) {
	log := workflow.GetLogger(ctx)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Hour,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    30 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    10 * time.Minute,
			MaximumAttempts:    3,
		},
	})
	if err := workflow.ExecuteActivity(ctx, ActivityMaintainActivityPartitions).Get(ctx, nil); err != nil {
		log.Error("ActivityArchiveWorkflow partition maintenance failed", "error", err)
		return nil, err
	}
	var partitions []string
	if err := workflow.ExecuteActivity(ctx, ActivityListArchivablePartitions).Get(ctx, &partitions); err != nil {
		log.Error("ActivityArchiveWorkflow listing archivable partitions failed", "error", err)
		return nil, err
	}
	result := &cronModels.ActivityArchiveWorkflowResult{Archived: []string{}}
	for _, p := range partitions {
		var exported cronModels.ActivityArchiveExportResult
		if err := workflow.ExecuteActivity(ctx, ActivityExportActivityPartition, cronModels.ActivityArchiveExportInput{Partition: p}).Get(ctx, &exported); err != nil {
			log.Error("ActivityArchiveWorkflow export failed", "partition", p, "error", err)
			return result, err
		}
		if err := workflow.ExecuteActivity(ctx, ActivityDropActivityPartition, exported).Get(ctx, nil); err != nil {
			log.Error("ActivityArchiveWorkflow drop failed", "partition", p, "error", err)
			return result, err
		}
		result.Archived = append(result.Archived, p)
	}
	log.Info("ActivityArchiveWorkflow completed", "archived", len(result.Archived))
	return result, nil
}
