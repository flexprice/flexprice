package subscription

import (
	"time"

	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowProcessRenewalDueAlert = "ProcessRenewalDueAlertWorkflow"
	ActivityProcessRenewalDueAlert = "ProcessRenewalDueAlertActivity"
)

// ProcessRenewalDueAlertWorkflow finds all subscriptions due for renewal and sends webhook events.
func ProcessRenewalDueAlertWorkflow(ctx workflow.Context, input subscriptionModels.ProcessRenewalDueAlertWorkflowInput) (*subscriptionModels.ProcessRenewalDueAlertWorkflowResult, error) {
	if err := input.Validate(); err != nil {
		return nil, err
	}

	logger := workflow.GetLogger(ctx)
	logger.Info("Starting process renewal due alert workflow")

	ao := workflow.ActivityOptions{
		StartToCloseTimeout: time.Hour,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    3,
		},
	}
	ctx = workflow.WithActivityOptions(ctx, ao)

	var result subscriptionModels.ProcessRenewalDueAlertActivityResult
	err := workflow.ExecuteActivity(ctx, ActivityProcessRenewalDueAlert, subscriptionModels.ProcessRenewalDueAlertActivityInput{}).Get(ctx, &result)
	if err != nil {
		logger.Error("Process renewal due alert workflow failed", "error", err)
		errStr := err.Error()
		return &subscriptionModels.ProcessRenewalDueAlertWorkflowResult{
			Success: false,
			Error:   &errStr,
		}, nil
	}

	logger.Info("Process renewal due alert workflow completed successfully")
	return &subscriptionModels.ProcessRenewalDueAlertWorkflowResult{
		Success: true,
	}, nil
}
