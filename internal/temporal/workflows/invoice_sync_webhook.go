package workflows

import (
	"errors"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"
)

const ActivityPublishInvoiceSyncWebhook = "PublishInvoiceSyncWebhookActivity"

// publishInvoiceSyncOutcome reports a provider sync's final outcome once; it never fails the workflow.
func publishInvoiceSyncOutcome(ctx workflow.Context, provider types.SecretProvider, invoiceID, tenantID, environmentID string, syncErr error) {
	input := models.InvoiceSyncWebhookInput{
		InvoiceID:     invoiceID,
		TenantID:      tenantID,
		EnvironmentID: environmentID,
		Provider:      provider,
	}
	if syncErr != nil {
		var appErr *temporal.ApplicationError
		if temporal.IsCanceledError(syncErr) || (errors.As(syncErr, &appErr) && appErr.Type() == ierr.ErrConnectionNotFound) {
			return
		}
		input.Error = invoiceSyncErrorMessage(syncErr)
	}

	ctx, _ = workflow.NewDisconnectedContext(ctx)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 3},
	})

	if err := workflow.ExecuteActivity(ctx, ActivityPublishInvoiceSyncWebhook, input).Get(ctx, nil); err != nil {
		workflow.GetLogger(ctx).Error("failed to publish invoice sync webhook", "error", err, "invoice_id", invoiceID, "provider", provider)
	}
}

// invoiceSyncErrorMessage drops Temporal's activity wrapping so the webhook carries the provider's message.
func invoiceSyncErrorMessage(err error) string {
	var appErr *temporal.ApplicationError
	if errors.As(err, &appErr) {
		return appErr.Message()
	}
	return err.Error()
}
