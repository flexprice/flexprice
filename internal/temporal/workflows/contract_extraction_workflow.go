package workflows

import (
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowContractExtraction           = "ContractExtractionWorkflow"
	ActivityExtractContract              = "ExtractContract"
	ActivityMarkContractExtractionFailed = "MarkContractExtractionFailed"

	defaultContractExtractionFailure = "Contract extraction failed"
)

func ContractExtractionWorkflow(ctx workflow.Context, input models.ContractExtractionWorkflowInput) error {
	logger := workflow.GetLogger(ctx)
	if err := input.Validate(); err != nil {
		logger.Error("Invalid workflow input", "error", err)
		return err
	}

	extractCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2.0,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	})
	err := workflow.ExecuteActivity(extractCtx, ActivityExtractContract, input).Get(ctx, nil)
	if err == nil {
		return nil
	}
	logger.Error("Contract extraction failed", "contract_id", input.ContractID, "error", err)

	failCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 5},
	})
	markInput := models.MarkContractExtractionFailedInput{ContractExtractionWorkflowInput: input, Reason: failureReason(err)}
	if markErr := workflow.ExecuteActivity(failCtx, ActivityMarkContractExtractionFailed, markInput).Get(ctx, nil); markErr != nil {
		logger.Error("Marking contract extraction failed did not succeed", "contract_id", input.ContractID, "error", markErr)
	}
	return err
}

func failureReason(err error) string {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) && appErr.Message() != "" {
		return appErr.Message()
	}
	return defaultContractExtractionFailure
}
