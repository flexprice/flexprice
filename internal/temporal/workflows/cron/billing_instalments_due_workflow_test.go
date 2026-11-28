package cron

import (
	"context"
	"errors"
	"testing"

	cronModels "github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func billDueInstalmentsStub(context.Context) error { return nil }

func TestBillingInstalmentsDueWorkflow(t *testing.T) {
	tests := []struct {
		name        string
		activityErr error
	}{
		{"completes when the activity succeeds", nil},
		{"fails when the activity keeps failing", errors.New("billing failed")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
			env.RegisterActivityWithOptions(billDueInstalmentsStub, activity.RegisterOptions{Name: ActivityBillDueInstalments})
			env.OnActivity(ActivityBillDueInstalments, mock.Anything).Return(tt.activityErr)

			env.ExecuteWorkflow(BillingInstalmentsDueWorkflow, cronModels.BillingInstalmentsDueWorkflowInput{})
			require.True(t, env.IsWorkflowCompleted())

			if tt.activityErr == nil {
				require.NoError(t, env.GetWorkflowError())
				return
			}

			require.Error(t, env.GetWorkflowError())
			env.AssertNumberOfCalls(t, ActivityBillDueInstalments, 3)
		})
	}
}
