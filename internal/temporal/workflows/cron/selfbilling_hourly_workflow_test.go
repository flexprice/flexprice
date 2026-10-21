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

func selfBillingHourlyStub(_ context.Context) error {
	return nil
}

func TestSelfBillingHourlyWorkflow(t *testing.T) {
	tests := []struct {
		name        string
		activityErr error
		wantErr     bool
	}{
		{
			name: "runs the activity once",
		},
		{
			name:        "activity error fails the workflow",
			activityErr: assert.AnError,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := &testsuite.WorkflowTestSuite{}
			env := ts.NewTestWorkflowEnvironment()

			env.RegisterActivityWithOptions(selfBillingHourlyStub, activity.RegisterOptions{
				Name: ActivitySelfBillingHourly,
			})
			env.OnActivity(ActivitySelfBillingHourly, mock.Anything).
				Return(tt.activityErr)

			env.ExecuteWorkflow(SelfBillingHourlyWorkflow, cronModels.SelfBillingHourlyWorkflowInput{})

			require.True(t, env.IsWorkflowCompleted())
			if tt.wantErr {
				require.Error(t, env.GetWorkflowError())
				return
			}

			require.NoError(t, env.GetWorkflowError())
			env.AssertNumberOfCalls(t, ActivitySelfBillingHourly, 1)
		})
	}
}
