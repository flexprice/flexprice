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

func dailyDraftAndComputeStub(_ context.Context, _ cronModels.DailyDraftAndComputeActivityInput) (*cronModels.DailyDraftAndComputeWorkflowResult, error) {
	return nil, nil
}

func TestDailyDraftAndComputeWorkflow_Success(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	expected := &cronModels.DailyDraftAndComputeWorkflowResult{
		TenantEnvsProcessed:   2,
		TotalDueSubscriptions: 5,
		TriggeredCount:        4,
		SkippedCount:          1,
	}

	env.RegisterActivityWithOptions(dailyDraftAndComputeStub, activity.RegisterOptions{
		Name: ActivityDailyDraftAndCompute,
	})
	env.OnActivity(ActivityDailyDraftAndCompute, mock.Anything, mock.Anything).Return(expected, nil)

	env.ExecuteWorkflow(DailyDraftAndComputeWorkflow, cronModels.DailyDraftAndComputeWorkflowInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result cronModels.DailyDraftAndComputeWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.Equal(t, 2, result.TenantEnvsProcessed)
	require.Equal(t, 5, result.TotalDueSubscriptions)
	require.Equal(t, 4, result.TriggeredCount)
	require.Equal(t, 1, result.SkippedCount)

	env.AssertExpectations(t)
}

func TestDailyDraftAndComputeWorkflow_ActivityError(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(dailyDraftAndComputeStub, activity.RegisterOptions{
		Name: ActivityDailyDraftAndCompute,
	})
	env.OnActivity(ActivityDailyDraftAndCompute, mock.Anything, mock.Anything).
		Return(nil, assert.AnError)

	env.ExecuteWorkflow(DailyDraftAndComputeWorkflow, cronModels.DailyDraftAndComputeWorkflowInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}
