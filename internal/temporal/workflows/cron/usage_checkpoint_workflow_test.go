package cron

import (
	"context"
	"sync"
	"testing"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func listUsageCheckpointEnvironmentsStub(_ context.Context) ([]cronModels.UsageCheckpointEnvironment, error) {
	return nil, nil
}

func usageCheckpointEnvironmentStub(_ context.Context, _ cronModels.UsageCheckpointEnvironmentInput) error {
	return nil
}

func TestUsageCheckpointWorkflow(t *testing.T) {
	environments := []cronModels.UsageCheckpointEnvironment{
		{TenantID: "t1", EnvironmentID: "e1"},
		{TenantID: "t1", EnvironmentID: "e2"},
	}
	tests := []struct {
		name      string
		failEnv   string
		wantErr   bool
		wantCalls int
	}{
		{name: "checkpoints every environment with one hour-aligned cutoff", wantCalls: 2},
		{name: "one failing environment fails the run but the others still run", failEnv: "e1", wantErr: true, wantCalls: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &testsuite.WorkflowTestSuite{}
			env := ts.NewTestWorkflowEnvironment()
			env.RegisterActivityWithOptions(listUsageCheckpointEnvironmentsStub, activity.RegisterOptions{Name: ActivityListUsageCheckpointEnvironments})
			env.RegisterActivityWithOptions(usageCheckpointEnvironmentStub, activity.RegisterOptions{Name: ActivityUsageCheckpointEnvironment})
			env.OnActivity(ActivityListUsageCheckpointEnvironments, mock.Anything).Return(environments, nil)

			var mu sync.Mutex
			var inputs []cronModels.UsageCheckpointEnvironmentInput
			env.OnActivity(ActivityUsageCheckpointEnvironment, mock.Anything, mock.Anything).Return(
				func(_ context.Context, input cronModels.UsageCheckpointEnvironmentInput) error {
					mu.Lock()
					inputs = append(inputs, input)
					mu.Unlock()
					if input.EnvironmentID == tt.failEnv {
						return assert.AnError
					}
					return nil
				})

			env.ExecuteWorkflow(UsageCheckpointWorkflow, cronModels.UsageCheckpointWorkflowInput{})

			require.True(t, env.IsWorkflowCompleted())
			if tt.wantErr {
				require.Error(t, env.GetWorkflowError())
			} else {
				require.NoError(t, env.GetWorkflowError())
			}
			ids := map[string]bool{}
			for _, input := range inputs {
				ids[input.EnvironmentID] = true
				require.True(t, input.Cutoff.Equal(inputs[0].Cutoff))
				require.Zero(t, input.Cutoff.Minute()+input.Cutoff.Second()+input.Cutoff.Nanosecond(), "cutoff is an hour boundary")
			}
			require.Len(t, ids, tt.wantCalls, "every environment ran once its activity attempts finished")
		})
	}
}
