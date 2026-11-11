package invoice

import (
	"context"
	"testing"

	invoiceModels "github.com/flexprice/flexprice/internal/temporal/models/invoice"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
)

func listPastPeriodDraftsStub(_ context.Context, _ invoiceModels.ListPastPeriodDraftsActivityInput) (*invoiceModels.ListPastPeriodDraftsActivityOutput, error) {
	return nil, nil
}

func createDraftForCurrentPeriodStub(_ context.Context, _ invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityInput) (*invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput, error) {
	return nil, nil
}

func computeInvoiceStub(_ context.Context, _ invoiceModels.ComputeInvoiceActivityInput) (*invoiceModels.ComputeInvoiceActivityOutput, error) {
	return nil, nil
}

func TestDraftAndComputeSubscriptionInvoiceWorkflow(t *testing.T) {
	tests := []struct {
		name            string
		recomputePast   bool
		pastDraftIDs    []string
		current         invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput
		failingIDs      []string
		wantListCalled  bool
		wantComputed    []string
		wantErr         bool
		wantSkipped     bool
		wantResultInvID string
	}{
		{
			name:            "flag off computes only the current draft",
			current:         invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput{InvoiceID: "inv_current"},
			wantComputed:    []string{"inv_current"},
			wantResultInvID: "inv_current",
		},
		{
			name:            "flag on computes past drafts then the current draft",
			recomputePast:   true,
			pastDraftIDs:    []string{"inv_past_1", "inv_past_2"},
			current:         invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput{InvoiceID: "inv_current"},
			wantListCalled:  true,
			wantComputed:    []string{"inv_past_1", "inv_past_2", "inv_current"},
			wantResultInvID: "inv_current",
		},
		{
			name:           "current period already invoiced still computes past drafts",
			recomputePast:  true,
			pastDraftIDs:   []string{"inv_past_1"},
			current:        invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput{Skipped: true},
			wantListCalled: true,
			wantComputed:   []string{"inv_past_1"},
			wantSkipped:    true,
		},
		{
			name:           "nothing to compute is skipped",
			recomputePast:  true,
			current:        invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput{Skipped: true},
			wantListCalled: true,
			wantSkipped:    true,
		},
		{
			name:           "a failing draft does not stop the rest and fails the workflow",
			recomputePast:  true,
			pastDraftIDs:   []string{"inv_past_1", "inv_past_2"},
			current:        invoiceModels.CreateDraftForCurrentSubscriptionPeriodActivityOutput{InvoiceID: "inv_current"},
			failingIDs:     []string{"inv_past_1"},
			wantListCalled: true,
			wantComputed:   []string{"inv_past_1", "inv_past_2", "inv_current"},
			wantErr:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := (&testsuite.WorkflowTestSuite{}).NewTestWorkflowEnvironment()
			env.RegisterActivityWithOptions(listPastPeriodDraftsStub, activity.RegisterOptions{Name: ActivityListPastPeriodDrafts})
			env.RegisterActivityWithOptions(createDraftForCurrentPeriodStub, activity.RegisterOptions{Name: ActivityCreateDraftForCurrentSubscriptionPeriod})
			env.RegisterActivityWithOptions(computeInvoiceStub, activity.RegisterOptions{Name: ActivityComputeInvoice})

			listCalled := false
			env.OnActivity(ActivityListPastPeriodDrafts, mock.Anything, mock.Anything).Return(
				func(_ context.Context, _ invoiceModels.ListPastPeriodDraftsActivityInput) (*invoiceModels.ListPastPeriodDraftsActivityOutput, error) {
					listCalled = true
					return &invoiceModels.ListPastPeriodDraftsActivityOutput{InvoiceIDs: tt.pastDraftIDs}, nil
				})
			env.OnActivity(ActivityCreateDraftForCurrentSubscriptionPeriod, mock.Anything, mock.Anything).Return(&tt.current, nil)

			var computed []string
			env.OnActivity(ActivityComputeInvoice, mock.Anything, mock.Anything).Return(
				func(_ context.Context, in invoiceModels.ComputeInvoiceActivityInput) (*invoiceModels.ComputeInvoiceActivityOutput, error) {
					computed = append(computed, in.InvoiceID)
					for _, id := range tt.failingIDs {
						if id == in.InvoiceID {
							return nil, temporal.NewNonRetryableApplicationError("compute failed", "ComputeFailed", nil)
						}
					}
					return &invoiceModels.ComputeInvoiceActivityOutput{}, nil
				})

			env.ExecuteWorkflow(DraftAndComputeSubscriptionInvoiceWorkflow, invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowInput{
				SubscriptionID:            "subs_1",
				TenantID:                  "tenant_1",
				EnvironmentID:             "env_1",
				UserID:                    "user_1",
				RecomputePastPeriodDrafts: tt.recomputePast,
			})

			require.True(t, env.IsWorkflowCompleted())
			require.Equal(t, tt.wantListCalled, listCalled)
			require.Equal(t, tt.wantComputed, computed)
			if tt.wantErr {
				require.Error(t, env.GetWorkflowError())
				return
			}
			require.NoError(t, env.GetWorkflowError())

			var result invoiceModels.DraftAndComputeSubscriptionInvoiceWorkflowResult
			require.NoError(t, env.GetWorkflowResult(&result))
			require.Equal(t, tt.wantSkipped, result.ComputeSkipped)
			require.Equal(t, tt.wantResultInvID, result.InvoiceID)
		})
	}
}
