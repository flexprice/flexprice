package service

import (
	"context"
	"errors"
	"testing"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	temporalclient "github.com/flexprice/flexprice/internal/temporal/client"
	"github.com/flexprice/flexprice/internal/temporal/models"
	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	subscriptionModels "github.com/flexprice/flexprice/internal/temporal/models/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
	"go.temporal.io/api/serviceerror"
)

type recordingClient struct {
	temporalclient.TemporalClient
	err     error
	options []models.StartWorkflowOptions
}

func (c *recordingClient) StartWorkflow(_ context.Context, options models.StartWorkflowOptions, _ interface{}, _ ...interface{}) (models.WorkflowRun, error) {
	c.options = append(c.options, options)
	return nil, c.err
}

func TestExecuteWorkflowExclusiveID(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")

	tests := []struct {
		name          string
		workflowType  types.TemporalWorkflowType
		params        interface{}
		wantID        string
		wantErrOnOpen bool
	}{
		{"plan price sync", types.TemporalPriceSyncV2Workflow, "plan_1", "wf_price_sync_plan_plan_1", true},
		{"addon price sync", types.TemporalAddonPriceSyncWorkflow, "addon_1", "wf_price_sync_addon_addon_1", true},
		{
			"subscription billing", types.TemporalProcessSubscriptionBillingWorkflow,
			subscriptionModels.ProcessSubscriptionBillingWorkflowInput{SubscriptionID: "sub_1", TenantID: "tenant_1", EnvironmentID: "env_1", PeriodStart: time.Now(), PeriodEnd: time.Now().Add(time.Hour)},
			"wf_process_subscription_billing_sub_1", true,
		},
		{
			"process invoice", types.TemporalProcessInvoiceWorkflow,
			invoiceModels.ProcessInvoiceWorkflowInput{InvoiceID: "inv_1", TenantID: "tenant_1", EnvironmentID: "env_1"},
			"wf_process_invoice_inv_1", true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &recordingClient{}
			svc := newTestService(tc, nil)

			for i := 0; i < 2; i++ {
				_, err := svc.ExecuteWorkflow(ctx, tt.workflowType, tt.params)
				require.NoError(t, err)
			}
			require.Len(t, tc.options, 2)
			require.Equal(t, tt.wantID, tc.options[0].ID)
			require.Equal(t, tt.wantID, tc.options[1].ID)
			require.Equal(t, tt.wantErrOnOpen, tc.options[0].WorkflowExecutionErrorWhenAlreadyStarted)
		})
	}
}

func TestExecuteWorkflowWithDelayAlreadyRunning(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")
	params := invoiceModels.ProcessInvoiceWorkflowInput{InvoiceID: "inv_1", TenantID: "tenant_1", EnvironmentID: "env_1"}

	tests := []struct {
		name        string
		err         error
		wantExisted bool
	}{
		{"already started", serviceerror.NewWorkflowExecutionAlreadyStarted("running", "", "run_1"), true},
		{"other error", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &recordingClient{err: tt.err}
			svc := newTestService(tc, nil)

			_, err := svc.ExecuteWorkflowWithDelay(ctx, types.TemporalProcessInvoiceWorkflow, params, 60)
			require.Error(t, err)
			require.Equal(t, tt.wantExisted, ierr.IsAlreadyExists(err))
			require.Equal(t, "wf_process_invoice_inv_1", tc.options[0].ID)
			require.True(t, tc.options[0].WorkflowExecutionErrorWhenAlreadyStarted)
		})
	}
}

func TestExecuteWorkflowExclusiveAlreadyRunning(t *testing.T) {
	ctx := types.SetEnvironmentID(types.SetTenantID(context.Background(), "tenant_1"), "env_1")
	tc := &recordingClient{err: serviceerror.NewWorkflowExecutionAlreadyStarted("running", "", "run_1")}
	svc := newTestService(tc, nil)

	_, err := svc.ExecuteWorkflow(ctx, types.TemporalPriceSyncV2Workflow, "plan_1")
	require.Error(t, err)
	require.True(t, ierr.IsAlreadyExists(err))
}
