package invoice

import (
	"context"
	"testing"
	"time"

	models "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func correctionActivity(context.Context, models.RecalculateInvoiceActivityInput) (*models.RecalculateInvoiceActivityOutput, error) {
	return nil, nil
}

func processingChild(ctx workflow.Context, _ models.ProcessInvoiceWorkflowInput) error {
	return workflow.Sleep(ctx, time.Hour)
}

func TestRecalculateWorkflowPreservesBodylessAndSelectedPaths(t *testing.T) {
	for _, ids := range [][]string{nil, {"selected_item"}} {
		name := "bodyless"
		if len(ids) > 0 {
			name = "selected sources"
		}
		t.Run(name, func(t *testing.T) {
			suite := &testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			env.RegisterActivityWithOptions(correctionActivity, activity.RegisterOptions{Name: ActivityRecalculateInvoice})
			env.RegisterWorkflowWithOptions(processingChild, workflow.RegisterOptions{Name: types.TemporalProcessInvoiceWorkflow.String()})
			input := models.RecalculateInvoiceWorkflowInput{InvoiceID: "old", TenantID: "tenant", EnvironmentID: "env", BillingScheduleItemIDs: ids}
			env.OnActivity(ActivityRecalculateInvoice, mock.Anything, mock.MatchedBy(func(request models.RecalculateInvoiceActivityInput) bool {
				return request.InvoiceID == input.InvoiceID && request.TenantID == input.TenantID && request.EnvironmentID == input.EnvironmentID && len(request.BillingScheduleItemIDs) == len(ids)
			})).Return(&models.RecalculateInvoiceActivityOutput{Success: true, InvoiceID: "replacement"}, nil).Once()
			started := 0
			env.SetOnChildWorkflowStartedListener(func(_ *workflow.Info, _ workflow.Context, args converter.EncodedValues) {
				var child models.ProcessInvoiceWorkflowInput
				require.NoError(t, args.Get(&child))
				require.Equal(t, "replacement", child.InvoiceID)
				require.True(t, child.SkipCompute)
				started++
			})
			env.ExecuteWorkflow(RecalculateInvoiceWorkflow, input)
			require.NoError(t, env.GetWorkflowError())
			require.Equal(t, len(ids), started)
			env.AssertExpectations(t)
		})
	}
}
