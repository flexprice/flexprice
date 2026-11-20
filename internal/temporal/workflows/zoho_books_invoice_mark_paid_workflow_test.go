package workflows

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
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

func syncCreditNoteToZohoStub(_ context.Context, _ models.ZohoBooksCreditNoteSyncWorkflowInput) error {
	return nil
}

func newCreditNoteSyncTestEnv() *testsuite.TestWorkflowEnvironment {
	ts := &testsuite.WorkflowTestSuite{}
	env := ts.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(syncCreditNoteToZohoStub, activity.RegisterOptions{
		Name: ActivitySyncCreditNoteToZoho,
	})
	return env
}

func TestZohoBooksCreditNoteSyncWorkflow_Success(t *testing.T) {
	env := newCreditNoteSyncTestEnv()
	input := models.ZohoBooksCreditNoteSyncWorkflowInput{CreditNoteID: "cn_001", TenantID: "tenant_001", EnvironmentID: "env_001"}
	env.OnActivity(ActivitySyncCreditNoteToZoho, mock.Anything, input).Return(nil).Once()

	env.ExecuteWorkflow(ZohoBooksCreditNoteSyncWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestZohoBooksCreditNoteSyncWorkflow_RetryableErrorRetriesThreeTimes(t *testing.T) {
	env := newCreditNoteSyncTestEnv()
	input := models.ZohoBooksCreditNoteSyncWorkflowInput{CreditNoteID: "cn_002", TenantID: "tenant_001", EnvironmentID: "env_001"}
	env.OnActivity(ActivitySyncCreditNoteToZoho, mock.Anything, input).Return(assert.AnError).Times(3)

	env.ExecuteWorkflow(ZohoBooksCreditNoteSyncWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestZohoBooksCreditNoteSyncWorkflow_NonRetryableErrorRunsOnce(t *testing.T) {
	env := newCreditNoteSyncTestEnv()
	input := models.ZohoBooksCreditNoteSyncWorkflowInput{CreditNoteID: "cn_003", TenantID: "tenant_001", EnvironmentID: "env_001"}
	env.OnActivity(ActivitySyncCreditNoteToZoho, mock.Anything, input).
		Return(temporal.NewNonRetryableApplicationError("Zoho Books rejected the credit note sync", "ZohoRequestRejected", assert.AnError)).Once()

	env.ExecuteWorkflow(ZohoBooksCreditNoteSyncWorkflow, input)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestZohoBooksCreditNoteSyncWorkflow_ValidationError(t *testing.T) {
	tests := []struct {
		name    string
		input   models.ZohoBooksCreditNoteSyncWorkflowInput
		wantErr string
	}{
		{"missing credit note id", models.ZohoBooksCreditNoteSyncWorkflowInput{TenantID: "tenant_001", EnvironmentID: "env_001"}, "credit_note_id is required"},
		{"missing tenant id", models.ZohoBooksCreditNoteSyncWorkflowInput{CreditNoteID: "cn_004", EnvironmentID: "env_001"}, "tenant_id is required"},
		{"missing environment id", models.ZohoBooksCreditNoteSyncWorkflowInput{CreditNoteID: "cn_004", TenantID: "tenant_001"}, "environment_id is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newCreditNoteSyncTestEnv()

			env.ExecuteWorkflow(ZohoBooksCreditNoteSyncWorkflow, tt.input)

			require.True(t, env.IsWorkflowCompleted())
			err := env.GetWorkflowError()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
