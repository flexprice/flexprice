package cron

import (
	"context"
	"testing"
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func recognizeRevenueStub(_ context.Context, _ cronModels.RecognizeRevenueActivityInput) error {
	return nil
}

func TestRevenueRecognitionWorkflow(t *testing.T) {
	start := time.Date(2026, 2, 1, 4, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		in    cronModels.RevenueRecognitionInput
		since time.Time
	}{
		{"billing window is the last week", cronModels.RevenueRecognitionInput{}, start.Add(-recognitionBillingLookback)},
		{"an explicit Since wins, even zero", cronModels.RevenueRecognitionInput{Since: &time.Time{}}, time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
			env.SetStartTime(start)
			env.RegisterActivityWithOptions(recognizeRevenueStub, activity.RegisterOptions{Name: ActivityRecognizeRevenue})

			var got cronModels.RecognizeRevenueActivityInput
			env.OnActivity(ActivityRecognizeRevenue, mock.Anything, mock.MatchedBy(func(in cronModels.RecognizeRevenueActivityInput) bool {
				got = in
				return true
			})).Return(nil)

			env.ExecuteWorkflow(RevenueRecognitionWorkflow, tt.in)
			require.True(t, env.IsWorkflowCompleted())
			require.NoError(t, env.GetWorkflowError())
			require.True(t, tt.since.Equal(got.Since), "since: want %s, got %s", tt.since, got.Since)
			require.True(t, start.Equal(got.AsOf), "as of the run time, got %s", got.AsOf)
		})
	}
}

func backfillRevenueStub(_ context.Context, _ cronModels.BackfillRevenueActivityInput) error {
	return nil
}

func TestRevenueRecognitionBackfillWorkflow(t *testing.T) {
	start := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.SetStartTime(start)
	env.RegisterActivityWithOptions(backfillRevenueStub, activity.RegisterOptions{Name: ActivityBackfillRevenue})

	var got cronModels.BackfillRevenueActivityInput
	env.OnActivity(ActivityBackfillRevenue, mock.Anything, mock.MatchedBy(func(in cronModels.BackfillRevenueActivityInput) bool {
		got = in
		return true
	})).Return(nil)

	env.ExecuteWorkflow(RevenueRecognitionBackfillWorkflow, cronModels.RevenueRecognitionBackfillInput{TenantID: "tenant_1", EnvironmentID: "env_1"})
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Equal(t, "tenant_1", got.TenantID)
	require.Equal(t, "env_1", got.EnvironmentID)
	require.True(t, start.Equal(got.AsOf), "as of the workflow start, got %s", got.AsOf)
}
