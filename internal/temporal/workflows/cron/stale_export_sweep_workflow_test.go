package cron

import (
	"context"
	"testing"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func sweepStaleExportRunsStub(context.Context) (int, error) { return 0, nil }

func TestStaleExportSweepWorkflow_RunsActivityOnce(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(sweepStaleExportRunsStub, activity.RegisterOptions{Name: ActivitySweepStaleExportRuns})
	env.OnActivity(ActivitySweepStaleExportRuns, mock.Anything).Return(2, nil)

	env.ExecuteWorkflow(StaleExportSweepWorkflow, cronModels.StaleExportSweepWorkflowInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertNumberOfCalls(t, ActivitySweepStaleExportRuns, 1)
}
