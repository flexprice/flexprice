package workflows

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func pullAndUpdatePaddleInvoiceStub(_ context.Context, _ models.PaddleInvoicePullSyncWorkflowInput) error {
	return nil
}

func TestPaddleInvoicePullSyncWorkflow_Success(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	input := models.PaddleInvoicePullSyncWorkflowInput{
		InvoiceID:     "inv_001",
		TenantID:      "tenant_001",
		EnvironmentID: "env_001",
	}

	env.RegisterActivityWithOptions(pullAndUpdatePaddleInvoiceStub, activity.RegisterOptions{
		Name: ActivityPullAndUpdatePaddleInvoice,
	})
	env.OnActivity(ActivityPullAndUpdatePaddleInvoice, mock.Anything, input).Return(nil)

	env.ExecuteWorkflow(PaddleInvoicePullSyncWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestPaddleInvoicePullSyncWorkflow_ActivityError(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	input := models.PaddleInvoicePullSyncWorkflowInput{
		InvoiceID:     "inv_002",
		TenantID:      "tenant_001",
		EnvironmentID: "env_001",
	}

	env.RegisterActivityWithOptions(pullAndUpdatePaddleInvoiceStub, activity.RegisterOptions{
		Name: ActivityPullAndUpdatePaddleInvoice,
	})
	env.OnActivity(ActivityPullAndUpdatePaddleInvoice, mock.Anything, input).
		Return(assert.AnError)

	env.ExecuteWorkflow(PaddleInvoicePullSyncWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

func TestPaddleInvoicePullSyncWorkflow_ValidationError(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	input := models.PaddleInvoicePullSyncWorkflowInput{
		InvoiceID:     "",
		TenantID:      "tenant_001",
		EnvironmentID: "env_001",
	}

	env.ExecuteWorkflow(PaddleInvoicePullSyncWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoice_id is required")
}
