package invoice

import (
	"context"
	"testing"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func createDraftStub(_ context.Context, _ invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityInput) (*invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput, error) {
	return nil, nil
}

func computeInvoiceStub(_ context.Context, _ invoiceModels.ComputeInvoiceActivityInput) (*invoiceModels.ComputeInvoiceActivityOutput, error) {
	return nil, nil
}

func TestDraftAndComputeSubscriptionInvoiceWorkflow_SkipsComputeWhenDraftSkipped(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(createDraftStub, activity.RegisterOptions{
		Name: ActivityCreateDraftForCurrentSubscriptionPeriod,
	})
	env.OnActivity(ActivityCreateDraftForCurrentSubscriptionPeriod, mock.Anything, mock.Anything).
		Return(&invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput{Skipped: true}, nil)
	// ComputeInvoiceActivity must NOT be called — not even registered here, so any attempt to
	// execute it would panic the test environment immediately.

	env.ExecuteWorkflow(DraftAndComputeSubscriptionInvoiceWorkflow, invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowInput{
		SubscriptionID:        "sub_1",
		TenantID:              "tenant_1",
		EnvironmentID:         "env_1",
		UserID:                "user_1",
		SkipIfAlreadyInvoiced: true,
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Success)
	require.True(t, result.ComputeSkipped)
	require.Empty(t, result.InvoiceID)

	env.AssertExpectations(t)
}

func TestDraftAndComputeSubscriptionInvoiceWorkflow_ComputesWhenDraftCreated(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(createDraftStub, activity.RegisterOptions{
		Name: ActivityCreateDraftForCurrentSubscriptionPeriod,
	})
	env.RegisterActivityWithOptions(computeInvoiceStub, activity.RegisterOptions{
		Name: ActivityComputeInvoice,
	})
	env.OnActivity(ActivityCreateDraftForCurrentSubscriptionPeriod, mock.Anything, mock.Anything).
		Return(&invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput{InvoiceID: "inv_1"}, nil)
	env.OnActivity(ActivityComputeInvoice, mock.Anything, mock.Anything).
		Return(&invoiceModels.ComputeInvoiceActivityOutput{Skipped: false}, nil)

	env.ExecuteWorkflow(DraftAndComputeSubscriptionInvoiceWorkflow, invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowInput{
		SubscriptionID: "sub_1",
		TenantID:       "tenant_1",
		EnvironmentID:  "env_1",
		UserID:         "user_1",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())

	var result invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowResult
	require.NoError(t, env.GetWorkflowResult(&result))
	require.True(t, result.Success)
	require.False(t, result.ComputeSkipped)
	require.Equal(t, "inv_1", result.InvoiceID)

	env.AssertExpectations(t)
}
