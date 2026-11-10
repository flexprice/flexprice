package workflows

import (
	"errors"
	"time"

	"github.com/flexprice/flexprice/internal/temporal/models"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const (
	WorkflowSendEmail       = "SendEmailWorkflow"
	ActivityDeliverEmail    = "DeliverEmail"
	ActivityMarkEmailFailed = "MarkEmailFailed"
)

// SendEmailWorkflow delivers one queued email. It first insists on every attachment for a short
// while, then sends without the ones it cannot produce; provider outages are retried, and the
// email is marked failed once retries run out.
func SendEmailWorkflow(ctx workflow.Context, in models.SendEmailWorkflowInput) error {
	if err := in.Validate(); err != nil {
		return err
	}

	withAttachments := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 2,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 10,
			BackoffCoefficient: 2.0,
			MaximumAttempts:    3,
		},
	})
	err := workflow.ExecuteActivity(withAttachments, ActivityDeliverEmail, models.DeliverEmailActivityInput{SendEmailWorkflowInput: in}).Get(ctx, nil)
	if err == nil || isNonRetryable(err) {
		return err
	}

	withoutMissing := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute * 2,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second * 5,
			BackoffCoefficient: 2.0,
			MaximumInterval:    time.Minute * 5,
			MaximumAttempts:    6,
		},
	})
	err = workflow.ExecuteActivity(withoutMissing, ActivityDeliverEmail, models.DeliverEmailActivityInput{SendEmailWorkflowInput: in, AllowMissingAttachments: true}).Get(ctx, nil)
	if err == nil || isNonRetryable(err) {
		return err
	}

	markCtx := workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 5},
	})
	if markErr := workflow.ExecuteActivity(markCtx, ActivityMarkEmailFailed, in).Get(ctx, nil); markErr != nil {
		return markErr
	}
	return err
}

// isNonRetryable reports an activity failure the activity already settled, such as a rejected email.
func isNonRetryable(err error) bool {
	var appErr *temporal.ApplicationError
	return errors.As(err, &appErr) && appErr.NonRetryable()
}
