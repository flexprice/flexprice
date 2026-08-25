package zoho

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func syncConfigWithApproval(enabled bool) *types.SyncConfig {
	return &types.SyncConfig{
		InvoiceSyncSettings: &types.InvoiceSyncSettings{SubmitForApproval: enabled},
	}
}

func TestAwaitingApproval(t *testing.T) {
	tests := []struct {
		status string
		want   bool
	}{
		{status: "draft", want: true},
		{status: "pending_approval", want: true},
		{status: "Draft", want: true},
		{status: "  draft  ", want: true},
		{status: "sent", want: false},
		{status: "approved", want: false},
		// Terminal, not awaiting: callers must stop rather than keep polling.
		{status: "rejected", want: false},
		{status: "paid", want: false},
		{status: "overdue", want: false},
		{status: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			assert.Equal(t, tt.want, awaitingApproval(tt.status))
		})
	}
}

func TestEnsureApprovedForPayment(t *testing.T) {
	tests := []struct {
		name           string
		syncConfig     *types.SyncConfig
		status         string
		statusSequence []string
		submitErr      error
		getInvoiceErr  error
		syncConfigErr  error

		wantPayable     bool
		wantErr         bool
		wantSubmitCalls int
		// wantGetInvoiceCalls counts only the poll-loop reads; the caller supplies the
		// initial invoice, so a short-circuit path polls zero times.
		wantGetInvoiceCalls int
	}{
		{
			// The regression that matters: an org with no approval flow also leaves
			// API-created invoices in draft, so the toggle must gate everything.
			name:                "toggle off leaves draft invoice payable",
			syncConfig:          syncConfigWithApproval(false),
			status:              "draft",
			wantPayable:         true,
			wantSubmitCalls:     0,
			wantGetInvoiceCalls: 0,
		},
		{
			name:                "no sync config at all leaves draft invoice payable",
			syncConfig:          nil,
			status:              "draft",
			wantPayable:         true,
			wantSubmitCalls:     0,
			wantGetInvoiceCalls: 0,
		},
		{
			name:                "draft submits and clears on second poll",
			syncConfig:          syncConfigWithApproval(true),
			status:              "draft",
			statusSequence:      []string{"draft", "approved"},
			wantPayable:         true,
			wantSubmitCalls:     1,
			wantGetInvoiceCalls: 2,
		},
		{
			name:                "draft submits and clears on first poll",
			syncConfig:          syncConfigWithApproval(true),
			status:              "draft",
			statusSequence:      []string{"sent"},
			wantPayable:         true,
			wantSubmitCalls:     1,
			wantGetInvoiceCalls: 1,
		},
		{
			name:                "never approved exhausts polls and is not payable",
			syncConfig:          syncConfigWithApproval(true),
			status:              "draft",
			statusSequence:      []string{"draft"},
			wantPayable:         false,
			wantSubmitCalls:     1,
			wantGetInvoiceCalls: approvalMaxPolls,
		},
		{
			// Rejection is terminal: stop immediately rather than polling out the clock.
			name:                "rejected before submit is terminal",
			syncConfig:          syncConfigWithApproval(true),
			status:              "rejected",
			wantPayable:         false,
			wantSubmitCalls:     0,
			wantGetInvoiceCalls: 0,
		},
		{
			name:                "rejected during polling is terminal",
			syncConfig:          syncConfigWithApproval(true),
			status:              "draft",
			statusSequence:      []string{"pending_approval", "rejected"},
			wantPayable:         false,
			wantSubmitCalls:     1,
			wantGetInvoiceCalls: 2,
		},
		{
			// Already in the approval queue: wait for it, but do not re-submit.
			name:                "pending approval polls without submitting",
			syncConfig:          syncConfigWithApproval(true),
			status:              "pending_approval",
			statusSequence:      []string{"pending_approval", "approved"},
			wantPayable:         true,
			wantSubmitCalls:     0,
			wantGetInvoiceCalls: 2,
		},
		{
			name:                "already sent short-circuits",
			syncConfig:          syncConfigWithApproval(true),
			status:              "sent",
			wantPayable:         true,
			wantSubmitCalls:     0,
			wantGetInvoiceCalls: 0,
		},
		{
			// Second pass of a retried activity: the invoice already cleared approval,
			// and re-submitting would make Zoho error.
			name:                "retry pass on approved invoice does not resubmit",
			syncConfig:          syncConfigWithApproval(true),
			status:              "approved",
			wantPayable:         true,
			wantSubmitCalls:     0,
			wantGetInvoiceCalls: 0,
		},
		{
			name:            "submit error propagates",
			syncConfig:      syncConfigWithApproval(true),
			status:          "draft",
			submitErr:       assert.AnError,
			wantErr:         true,
			wantSubmitCalls: 1,
		},
		{
			name:            "poll read error propagates",
			syncConfig:      syncConfigWithApproval(true),
			status:          "draft",
			getInvoiceErr:   assert.AnError,
			wantErr:         true,
			wantSubmitCalls: 1,
		},
		{
			name:          "sync config error propagates",
			syncConfigErr: assert.AnError,
			status:        "draft",
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeZohoClient{
				getInvoiceResp: &InvoiceResponse{
					InvoiceID:  "zoho_inv_1",
					CustomerID: "zoho_cust_1",
				},
				getInvoiceErr:  tt.getInvoiceErr,
				statusSequence: tt.statusSequence,
				submitErr:      tt.submitErr,
				syncConfig:     tt.syncConfig,
				syncConfigErr:  tt.syncConfigErr,
			}
			svc := newTestInvoiceService(client, &fakeMappingRepo{})

			payable, err := svc.ensureApprovedForPaymentWithin(context.Background(), "inv_1", &InvoiceResponse{
				InvoiceID:  "zoho_inv_1",
				CustomerID: "zoho_cust_1",
				Status:     tt.status,
			}, 0, approvalMaxPolls)

			if tt.wantErr {
				require.Error(t, err)
				assert.Nil(t, payable)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantPayable, payable != nil)
				assert.Equal(t, tt.wantGetInvoiceCalls, client.getInvoiceCalls, "poll reads")
			}
			assert.Equal(t, tt.wantSubmitCalls, client.submitCalls, "submit calls")
		})
	}
}

// Zoho can settle the invoice (inbound webhook, or a human) during the approval wait, so
// the caller must settle against the post-poll read rather than the stale pre-wait one.
func TestEnsureApprovedForPayment_ReturnsPostPollInvoice(t *testing.T) {
	tests := []struct {
		name        string
		pollBalance decimal.Decimal
	}{
		{name: "balance reduced during approval", pollBalance: decimal.NewFromInt(60)},
		{name: "balance settled during approval", pollBalance: decimal.Zero},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeZohoClient{
				getInvoiceResp: &InvoiceResponse{
					InvoiceID:  "zoho_inv_1",
					CustomerID: "zoho_cust_1",
				},
				statusSequence:  []string{"approved"},
				balanceSequence: []decimal.Decimal{tt.pollBalance},
				syncConfig:      syncConfigWithApproval(true),
			}
			svc := newTestInvoiceService(client, &fakeMappingRepo{})

			payable, err := svc.ensureApprovedForPaymentWithin(context.Background(), "inv_1", &InvoiceResponse{
				InvoiceID:  "zoho_inv_1",
				CustomerID: "zoho_cust_1",
				Status:     InvoiceStatusDraft,
				Balance:    decimal.NewFromInt(100),
			}, 0, approvalMaxPolls)

			require.NoError(t, err)
			require.NotNil(t, payable)
			assert.True(t, tt.pollBalance.Equal(payable.Balance),
				"want post-poll balance %s, got %s", tt.pollBalance, payable.Balance)
		})
	}
}

func TestEnsureApprovedForPayment_NilInvoice(t *testing.T) {
	client := &fakeZohoClient{syncConfig: syncConfigWithApproval(true)}
	svc := newTestInvoiceService(client, &fakeMappingRepo{})

	payable, err := svc.ensureApprovedForPaymentWithin(context.Background(), "inv_1", nil, 0, approvalMaxPolls)

	require.NoError(t, err)
	assert.Nil(t, payable)
	assert.Equal(t, 0, client.submitCalls)
}
