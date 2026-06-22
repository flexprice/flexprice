package cron

import (
	"context"
	"testing"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func fetchAndTriggerStub(_ context.Context) (*cronModels.PaddleInvoicePullSyncCronResult, error) {
	return nil, nil
}

func TestPaddleInvoicePullSyncCronWorkflow_Success(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	expected := &cronModels.PaddleInvoicePullSyncCronResult{
		Total:     3,
		Triggered: 2,
		Failed:    1,
	}

	env.RegisterActivityWithOptions(fetchAndTriggerStub, activity.RegisterOptions{
		Name: ActivityFetchAndTriggerPaddleInvoicePullSync,
	})
	env.OnActivity(ActivityFetchAndTriggerPaddleInvoicePullSync, mock.Anything).Return(expected, nil)

	env.ExecuteWorkflow(PaddleInvoicePullSyncCronWorkflow, cronModels.PaddleInvoicePullSyncCronInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result cronModels.PaddleInvoicePullSyncCronResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 3, result.Total)
	require.Equal(t, 2, result.Triggered)
	require.Equal(t, 1, result.Failed)

	env.AssertExpectations(t)
}

func TestPaddleInvoicePullSyncCronWorkflow_ActivityError(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(fetchAndTriggerStub, activity.RegisterOptions{
		Name: ActivityFetchAndTriggerPaddleInvoicePullSync,
	})
	env.OnActivity(ActivityFetchAndTriggerPaddleInvoicePullSync, mock.Anything).
		Return(nil, assert.AnError)

	env.ExecuteWorkflow(PaddleInvoicePullSyncCronWorkflow, cronModels.PaddleInvoicePullSyncCronInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}
