package workflows

import (
	"context"
	"errors"
	"fmt"
	"testing"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/models"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func TestStripeInvoiceSyncWorkflow_PublishesFinalOutcomeOnce(t *testing.T) {
	tests := []struct {
		name         string
		syncErr      error
		wantAttempts int
		wantPublish  bool
		wantError    string
	}{
		{name: "success", wantAttempts: 1, wantPublish: true},
		{name: "retryable failure publishes after last attempt", syncErr: errors.New("stripe down"), wantAttempts: 3, wantPublish: true, wantError: "stripe down"},
		{name: "wrapped failure keeps a clean message", syncErr: fmt.Errorf("create stripe invoice: %w", errors.New("no such customer")), wantAttempts: 3, wantPublish: true, wantError: "create stripe invoice: no such customer"},
		{name: "non-retryable failure", syncErr: temporal.NewNonRetryableApplicationError("bad address", "InvoiceValidationError", nil), wantAttempts: 1, wantPublish: true, wantError: "bad address"},
		{name: "missing connection is not published", syncErr: temporal.NewNonRetryableApplicationError("not configured", ierr.ErrConnectionNotFound, nil), wantAttempts: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()

			attempts := 0
			env.RegisterActivityWithOptions(func(context.Context, models.StripeInvoiceSyncWorkflowInput) error {
				attempts++
				return tt.syncErr
			}, activity.RegisterOptions{Name: ActivitySyncInvoiceToStripe})

			var published []models.InvoiceSyncWebhookInput
			env.RegisterActivityWithOptions(func(_ context.Context, in models.InvoiceSyncWebhookInput) error {
				published = append(published, in)
				return nil
			}, activity.RegisterOptions{Name: ActivityPublishInvoiceSyncWebhook})

			env.ExecuteWorkflow(StripeInvoiceSyncWorkflow, models.StripeInvoiceSyncWorkflowInput{
				InvoiceID: "inv_1", TenantID: "tenant_1", EnvironmentID: "env_1",
			})

			require.True(t, env.IsWorkflowCompleted())
			require.Equal(t, tt.syncErr == nil, env.GetWorkflowError() == nil)
			require.Equal(t, tt.wantAttempts, attempts)
			if !tt.wantPublish {
				require.Empty(t, published)
				return
			}
			require.Len(t, published, 1)
			require.Equal(t, models.InvoiceSyncWebhookInput{
				InvoiceID: "inv_1", TenantID: "tenant_1", EnvironmentID: "env_1",
				Provider: types.SecretProviderStripe, Error: tt.wantError,
			}, published[0])
		})
	}
}

func TestInvoiceSyncWorkflow_PublishFailureDoesNotFailWorkflow(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, models.StripeInvoiceSyncWorkflowInput) error {
		return nil
	}, activity.RegisterOptions{Name: ActivitySyncInvoiceToStripe})
	env.RegisterActivityWithOptions(func(context.Context, models.InvoiceSyncWebhookInput) error {
		return errors.New("kafka down")
	}, activity.RegisterOptions{Name: ActivityPublishInvoiceSyncWebhook})

	env.ExecuteWorkflow(StripeInvoiceSyncWorkflow, models.StripeInvoiceSyncWorkflowInput{
		InvoiceID: "inv_1", TenantID: "tenant_1", EnvironmentID: "env_1",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
}

// HubSpot swallows sync failures, yet the failure must still be reported.
func TestHubSpotInvoiceSyncWorkflow_PublishesSwallowedFailure(t *testing.T) {
	env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, models.HubSpotInvoiceSyncWorkflowInput) error {
		return temporal.NewNonRetryableApplicationError("hubspot rejected", "HubSpotError", nil)
	}, activity.RegisterOptions{Name: ActivitySyncInvoiceToHubSpot})

	var published []models.InvoiceSyncWebhookInput
	env.RegisterActivityWithOptions(func(_ context.Context, in models.InvoiceSyncWebhookInput) error {
		published = append(published, in)
		return nil
	}, activity.RegisterOptions{Name: ActivityPublishInvoiceSyncWebhook})

	env.ExecuteWorkflow(HubSpotInvoiceSyncWorkflow, models.HubSpotInvoiceSyncWorkflowInput{
		InvoiceID: "inv_1", TenantID: "tenant_1", EnvironmentID: "env_1",
	})

	require.True(t, env.IsWorkflowCompleted())
	require.NoError(t, env.GetWorkflowError())
	require.Len(t, published, 1)
	require.Equal(t, types.SecretProviderHubSpot, published[0].Provider)
	require.Equal(t, "hubspot rejected", published[0].Error)
}
