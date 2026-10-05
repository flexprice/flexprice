package cron

import (
	"context"
	"testing"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func maintainPartitionsStub(_ context.Context) error { return nil }

func listArchivablePartitionsStub(_ context.Context) ([]string, error) { return nil, nil }

func exportActivityPartitionStub(_ context.Context, _ cronModels.ActivityArchiveExportInput) (*cronModels.ActivityArchiveExportResult, error) {
	return nil, nil
}

func dropActivityPartitionStub(_ context.Context, _ cronModels.ActivityArchiveExportResult) error { return nil }

func newActivityArchiveEnv() *testsuite.TestWorkflowEnvironment {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(maintainPartitionsStub, activity.RegisterOptions{Name: ActivityMaintainActivityPartitions})
	env.RegisterActivityWithOptions(listArchivablePartitionsStub, activity.RegisterOptions{Name: ActivityListArchivablePartitions})
	env.RegisterActivityWithOptions(exportActivityPartitionStub, activity.RegisterOptions{Name: ActivityExportActivityPartition})
	env.RegisterActivityWithOptions(dropActivityPartitionStub, activity.RegisterOptions{Name: ActivityDropActivityPartition})
	return env
}

func TestActivityArchiveWorkflow_MaintainsThenExportsAndDropsEachPartition(t *testing.T) {
	env := newActivityArchiveEnv()

	var calls []string
	env.OnActivity(ActivityMaintainActivityPartitions, mock.Anything).
		Run(func(mock.Arguments) { calls = append(calls, "maintain") }).Return(nil).Once()
	env.OnActivity(ActivityListArchivablePartitions, mock.Anything).
		Run(func(mock.Arguments) { calls = append(calls, "list") }).
		Return([]string{"activity_logs_2026_05", "activity_logs_2026_06"}, nil).Once()
	env.OnActivity(ActivityExportActivityPartition, mock.Anything, mock.Anything).
		Return(func(_ context.Context, in cronModels.ActivityArchiveExportInput) (*cronModels.ActivityArchiveExportResult, error) {
			calls = append(calls, "export "+in.Partition)
			return &cronModels.ActivityArchiveExportResult{Partition: in.Partition, Counts: map[string]int{"t": 1}}, nil
		}).Twice()
	env.OnActivity(ActivityDropActivityPartition, mock.Anything, mock.Anything).
		Return(func(_ context.Context, in cronModels.ActivityArchiveExportResult) error {
			calls = append(calls, "drop "+in.Partition)
			return nil
		}).Twice()

	env.ExecuteWorkflow(ActivityArchiveWorkflow, cronModels.ActivityArchiveWorkflowInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
	require.Equal(t, []string{
		"maintain", "list",
		"export activity_logs_2026_05", "drop activity_logs_2026_05",
		"export activity_logs_2026_06", "drop activity_logs_2026_06",
	}, calls)

	var result cronModels.ActivityArchiveWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, []string{"activity_logs_2026_05", "activity_logs_2026_06"}, result.Archived)
}

func TestActivityArchiveWorkflow_ExportFailureNeverDrops(t *testing.T) {
	env := newActivityArchiveEnv()

	env.OnActivity(ActivityMaintainActivityPartitions, mock.Anything).Return(nil).Once()
	env.OnActivity(ActivityListArchivablePartitions, mock.Anything).Return([]string{"activity_logs_2026_05"}, nil).Once()
	env.OnActivity(ActivityExportActivityPartition, mock.Anything, mock.Anything).
		Return(nil, temporal.NewNonRetryableApplicationError("upload failed", "test", nil)).Once()

	env.ExecuteWorkflow(ActivityArchiveWorkflow, cronModels.ActivityArchiveWorkflowInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	env.AssertExpectations(t)
	env.AssertNotCalled(t, ActivityDropActivityPartition, mock.Anything, mock.Anything)
}

func TestActivityArchiveWorkflow_MaintenanceFailureStopsRun(t *testing.T) {
	env := newActivityArchiveEnv()

	env.OnActivity(ActivityMaintainActivityPartitions, mock.Anything).
		Return(temporal.NewNonRetryableApplicationError("ddl failed", "test", nil)).Once()

	env.ExecuteWorkflow(ActivityArchiveWorkflow, cronModels.ActivityArchiveWorkflowInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	env.AssertNotCalled(t, ActivityListArchivablePartitions, mock.Anything)
}
