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

func markZohoBooksInvoicePaidStub(_ context.Context, _ models.ZohoBooksInvoiceMarkPaidWorkflowInput) error {
	return nil
}

func TestZohoBooksInvoiceMarkPaidWorkflow_Success(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	input := models.NewZohoBooksInvoiceMarkPaidWorkflowInput("inv_001", "tenant_001", "env_001")

	env.RegisterActivityWithOptions(markZohoBooksInvoicePaidStub, activity.RegisterOptions{
		Name: ActivityMarkZohoBooksInvoicePaid,
	})
	env.OnActivity(ActivityMarkZohoBooksInvoicePaid, mock.Anything, input).Return(nil)

	env.ExecuteWorkflow(ZohoBooksInvoiceMarkPaidWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestZohoBooksInvoiceMarkPaidWorkflow_ActivityError(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	input := models.NewZohoBooksInvoiceMarkPaidWorkflowInput("inv_002", "tenant_001", "env_001")

	env.RegisterActivityWithOptions(markZohoBooksInvoicePaidStub, activity.RegisterOptions{
		Name: ActivityMarkZohoBooksInvoicePaid,
	})
	env.OnActivity(ActivityMarkZohoBooksInvoicePaid, mock.Anything, input).
		Return(assert.AnError)

	env.ExecuteWorkflow(ZohoBooksInvoiceMarkPaidWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}

func TestZohoBooksInvoiceMarkPaidWorkflow_ValidationError(t *testing.T) {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()

	input := models.NewZohoBooksInvoiceMarkPaidWorkflowInput("", "tenant_001", "env_001")

	env.ExecuteWorkflow(ZohoBooksInvoiceMarkPaidWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	err := env.GetWorkflowError()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invoice_id is required")
}
