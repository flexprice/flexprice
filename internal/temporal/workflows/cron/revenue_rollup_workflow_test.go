package cron

import (
	"context"
	"testing"
	"time"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func rollupDirtyStub(_ context.Context, _ cronModels.RollupDirtyActivityInput) (*cronModels.RevenueRollupWorkflowResult, error) {
	return nil, nil
}

func reconcileBookedInvoicesStub(_ context.Context, _ time.Time) (*cronModels.RevenueSweepResult, error) {
	return nil, nil
}

// registerSweepOK registers the sweep activity with a permissive expectation —
// tests that assert on the rollup window don't care about the sweep's own.
func registerSweepOK(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(reconcileBookedInvoicesStub, activity.RegisterOptions{Name: ActivityReconcileBookedInvoices})
	env.OnActivity(ActivityReconcileBookedInvoices, mock.Anything, mock.Anything).
		Return(&cronModels.RevenueSweepResult{}, nil).Maybe()
}

func TestRevenueRollupWorkflow_Success(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	expected := &cronModels.RevenueRollupWorkflowResult{Rolled: 5, Skipped: 1}
	input := cronModels.RevenueRollupInput{Interval: 2 * time.Hour}

	env.RegisterActivityWithOptions(rollupDirtyStub, activity.RegisterOptions{
		Name: ActivityRollupDirty,
	})
	registerSweepOK(env)

	var capturedSince time.Time
	env.OnActivity(ActivityRollupDirty, mock.Anything, mock.MatchedBy(func(in cronModels.RollupDirtyActivityInput) bool {
		since := in.Since
		capturedSince = since
		return true
	})).Return(expected, nil)

	beforeStart := time.Now()
	env.ExecuteWorkflow(RevenueRollupWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)

	// The activity must be invoked with the run window: since = workflow start - Interval.
	require.False(t, capturedSince.IsZero(), "RollupDirtyActivity must receive a non-zero since")
	require.WithinDuration(t, beforeStart.Add(-input.Interval), capturedSince, 5*time.Second)
}

func TestRevenueRollupWorkflow_DefaultInterval(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(rollupDirtyStub, activity.RegisterOptions{
		Name: ActivityRollupDirty,
	})
	registerSweepOK(env)

	var capturedSince time.Time
	env.OnActivity(ActivityRollupDirty, mock.Anything, mock.MatchedBy(func(in cronModels.RollupDirtyActivityInput) bool {
		since := in.Since
		capturedSince = since
		return true
	})).Return(&cronModels.RevenueRollupWorkflowResult{}, nil)

	beforeStart := time.Now()
	// Zero Interval must fall back to defaultRevenueRollupInterval (24h).
	env.ExecuteWorkflow(RevenueRollupWorkflow, cronModels.RevenueRollupInput{})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	require.WithinDuration(t, beforeStart.Add(-defaultRevenueRollupInterval), capturedSince, 5*time.Second)
}

func TestRevenueRollupWorkflow_ActivityError(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(rollupDirtyStub, activity.RegisterOptions{
		Name: ActivityRollupDirty,
	})
	registerSweepOK(env)
	env.OnActivity(ActivityRollupDirty, mock.Anything, mock.Anything).
		Return(nil, assert.AnError)

	env.ExecuteWorkflow(RevenueRollupWorkflow, cronModels.RevenueRollupInput{Interval: time.Hour})

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

// TestRevenueRollupWorkflow_ExplicitSince proves a manual/backfill run: an
// explicit Since in the input wins over the schedule-derived window.
func TestRevenueRollupWorkflow_ExplicitSince(t *testing.T) {
	suite := &testsuite.WorkflowTestSuite{}
	env := suite.NewTestWorkflowEnvironment()

	want := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	input := cronModels.RevenueRollupInput{Interval: 2 * time.Hour, Since: &want}

	env.RegisterActivityWithOptions(rollupDirtyStub, activity.RegisterOptions{
		Name: ActivityRollupDirty,
	})
	registerSweepOK(env)

	var capturedSince time.Time
	env.OnActivity(ActivityRollupDirty, mock.Anything, mock.MatchedBy(func(in cronModels.RollupDirtyActivityInput) bool {
		since := in.Since
		capturedSince = since
		return true
	})).Return(&cronModels.RevenueRollupWorkflowResult{}, nil)

	env.ExecuteWorkflow(RevenueRollupWorkflow, input)
	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.True(t, capturedSince.Equal(want), "explicit Since must be passed through verbatim, got %s", capturedSince)
}

// TestRevenueRollupWorkflow_SweepWindow proves the sweep runs after the
// rollup with the wider lookback window.
func TestRevenueRollupWorkflow_SweepWindow(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(rollupDirtyStub, activity.RegisterOptions{Name: ActivityRollupDirty})
	env.RegisterActivityWithOptions(reconcileBookedInvoicesStub, activity.RegisterOptions{Name: ActivityReconcileBookedInvoices})
	env.OnActivity(ActivityRollupDirty, mock.Anything, mock.Anything).
		Return(&cronModels.RevenueRollupWorkflowResult{}, nil)

	var sweepSince time.Time
	env.OnActivity(ActivityReconcileBookedInvoices, mock.Anything, mock.MatchedBy(func(since time.Time) bool {
		sweepSince = since
		return true
	})).Return(&cronModels.RevenueSweepResult{Checked: 2, Drifted: 1}, nil)

	beforeStart := time.Now()
	env.ExecuteWorkflow(RevenueRollupWorkflow, cronModels.RevenueRollupInput{Interval: time.Hour})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.WithinDuration(t, beforeStart.Add(-driftSweepLookback), sweepSince, 5*time.Second,
		"the sweep must window on the wider lookback, not the rollup interval")
}

// TestRevenueRollupWorkflow_ResumeRequiresItsWindow: a cursor resumes a run
// whose scan window was its own. Deriving `since` from the new run's schedule
// would check the not-yet-processed subscriptions against a different window,
// and usage ingested between the two would be missed for exactly those.
func TestRevenueRollupWorkflow_ResumeRequiresItsWindow(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(rollupDirtyStub, activity.RegisterOptions{Name: ActivityRollupDirty})
	env.RegisterActivityWithOptions(reconcileBookedInvoicesStub, activity.RegisterOptions{Name: ActivityReconcileBookedInvoices})

	env.ExecuteWorkflow(RevenueRollupWorkflow, cronModels.RevenueRollupInput{
		ResumeEnvironmentID:       "env_1",
		ResumeAfterSubscriptionID: "sub_abc",
	})

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err, "resuming without the original window must fail rather than silently rescan")
	assert.Contains(t, err.Error(), "resuming requires the original scan window")
}
