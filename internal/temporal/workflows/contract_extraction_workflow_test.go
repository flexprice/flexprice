package workflows

import (
	"context"
	"errors"
	"testing"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func extractContractStub(_ context.Context, _ models.ContractExtractionWorkflowInput) error {
	return nil
}

func markContractExtractionFailedStub(_ context.Context, _ models.MarkContractExtractionFailedInput) error {
	return nil
}

func contractExtractionEnv() *testsuite.TestWorkflowEnvironment {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(extractContractStub, activity.RegisterOptions{Name: ActivityExtractContract})
	env.RegisterActivityWithOptions(markContractExtractionFailedStub, activity.RegisterOptions{Name: ActivityMarkContractExtractionFailed})
	return env
}

var contractExtractionInput = models.ContractExtractionWorkflowInput{
	ContractID:    "contract_001",
	TenantID:      "tenant_001",
	EnvironmentID: "env_001",
}

func TestContractExtractionWorkflow_Success(t *testing.T) {
	env := contractExtractionEnv()
	env.OnActivity(ActivityExtractContract, mock.Anything, contractExtractionInput).Return(nil).Once()

	env.ExecuteWorkflow(ContractExtractionWorkflow, contractExtractionInput)

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	env.AssertExpectations(t)
	env.AssertNotCalled(t, ActivityMarkContractExtractionFailed, mock.Anything, mock.Anything)
}

func TestContractExtractionWorkflow_FailureMarksContract(t *testing.T) {
	env := contractExtractionEnv()
	env.OnActivity(ActivityExtractContract, mock.Anything, contractExtractionInput).
		Return(temporal.NewNonRetryableApplicationError("Upload a PDF, PNG or JPG file", "ContractExtractionError", errors.New("bad type"))).Once()
	env.OnActivity(ActivityMarkContractExtractionFailed, mock.Anything, models.MarkContractExtractionFailedInput{
		ContractExtractionWorkflowInput: contractExtractionInput,
		Reason:                          "Upload a PDF, PNG or JPG file",
	}).Return(nil).Once()

	env.ExecuteWorkflow(ContractExtractionWorkflow, contractExtractionInput)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestContractExtractionWorkflow_RetriesThenMarksFailed(t *testing.T) {
	env := contractExtractionEnv()
	env.OnActivity(ActivityExtractContract, mock.Anything, contractExtractionInput).
		Return(temporal.NewApplicationError("AI provider rate limit exceeded. Wait and retry.", "ContractExtractionError")).Times(3)
	env.OnActivity(ActivityMarkContractExtractionFailed, mock.Anything, mock.MatchedBy(func(in models.MarkContractExtractionFailedInput) bool {
		return in.ContractID == contractExtractionInput.ContractID && in.Reason == "AI provider rate limit exceeded. Wait and retry."
	})).Return(nil).Once()

	env.ExecuteWorkflow(ContractExtractionWorkflow, contractExtractionInput)

	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
	env.AssertExpectations(t)
}

func TestContractExtractionWorkflow_InvalidInput(t *testing.T) {
	env := contractExtractionEnv()
	env.ExecuteWorkflow(ContractExtractionWorkflow, models.ContractExtractionWorkflowInput{TenantID: "t", EnvironmentID: "e"})
	require.True(t, env.IsWorkflowCompleted())
	require.Error(t, env.GetWorkflowError())
}
