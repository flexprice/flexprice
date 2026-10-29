package invoice

import (
	"context"
	"testing"

	models "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"
)

func processingCompute(context.Context, models.ComputeInvoiceActivityInput) (*models.ComputeInvoiceActivityOutput, error) {
	return nil, nil
}

func processingFinalize(context.Context, models.FinalizeInvoiceActivityInput) (*models.FinalizeInvoiceActivityOutput, error) {
	return nil, nil
}

func processingPayment(context.Context, models.PaymentActivityInput) (*models.PaymentActivityOutput, error) {
	return nil, nil
}

func TestProcessInvoiceMaterializedDraft(t *testing.T) {
	for _, test := range []struct {
		name        string
		version     workflow.Version
		skipCompute bool
		delay       bool
	}{
		{name: "ordinary invoice still computes", version: 1},
		{name: "materialized invoice proceeds to payment", version: 1, skipCompute: true},
		{name: "materialized invoice respects delay", version: 1, skipCompute: true, delay: true},
		{name: "old history retains compute", version: workflow.DefaultVersion, skipCompute: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			suite := &testsuite.WorkflowTestSuite{}
			env := suite.NewTestWorkflowEnvironment()
			env.RegisterActivityWithOptions(processingCompute, activity.RegisterOptions{Name: ActivityComputeInvoice})
			env.RegisterActivityWithOptions(processingFinalize, activity.RegisterOptions{Name: ActivityFinalizeInvoice})
			env.RegisterActivityWithOptions(processingPayment, activity.RegisterOptions{Name: ActivityAttemptInvoicePayment})
			env.OnGetVersion("materialized-invoice-processing", workflow.DefaultVersion, 1).Return(test.version)
			if !test.skipCompute || test.version == workflow.DefaultVersion {
				env.OnActivity(ActivityComputeInvoice, mock.Anything, mock.Anything).Return(&models.ComputeInvoiceActivityOutput{}, nil).Once()
			}
			env.OnActivity(ActivityFinalizeInvoice, mock.Anything, mock.Anything).Return(&models.FinalizeInvoiceActivityOutput{Success: true, Skipped: test.delay}, nil).Once()
			if !test.delay {
				env.OnActivity(ActivityAttemptInvoicePayment, mock.Anything, mock.Anything).Return(&models.PaymentActivityOutput{Success: true}, nil).Once()
			}
			env.ExecuteWorkflow(ProcessInvoiceWorkflow, models.ProcessInvoiceWorkflowInput{InvoiceID: "invoice", TenantID: "tenant", EnvironmentID: "environment", SkipCompute: test.skipCompute})
			require.NoError(t, env.GetWorkflowError())
			env.AssertExpectations(t)
		})
	}
}
