package chargebee

import (
	"context"
	"testing"

	transactionModel "github.com/chargebee/chargebee-go/v3/models/transaction"
	transactionEnum "github.com/chargebee/chargebee-go/v3/models/transaction/enum"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type stubRefundClient struct {
	ChargebeeClient
	gotTransactionID  string
	gotAmountMinor    int64
	gotIdempotencyKey string
	result            *transactionModel.Transaction
	err               error
}

func (c *stubRefundClient) RefundTransaction(_ context.Context, transactionID string, amountMinor int64, idempotencyKey string) (*transactionModel.Transaction, error) {
	c.gotTransactionID = transactionID
	c.gotAmountMinor = amountMinor
	c.gotIdempotencyKey = idempotencyKey
	return c.result, c.err
}

func TestChargebeeRefundAdapter_NormalizesStatus(t *testing.T) {
	tests := []struct {
		name        string
		status      transactionEnum.Status
		wantStatus  types.RefundStatus
		wantSettled string
	}{
		{"success settles inline", transactionEnum.StatusSuccess, types.RefundStatusSucceeded, "12.5"},
		{"in progress stays in flight", transactionEnum.StatusInProgress, types.RefundStatusProcessing, "0"},
		{"failure is terminal", transactionEnum.StatusFailure, types.RefundStatusFailed, "0"},
		{"voided is terminal", transactionEnum.StatusVoided, types.RefundStatusFailed, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubRefundClient{result: &transactionModel.Transaction{
				Id:     "txn_refund_1",
				Amount: 1250,
				Status: tt.status,
			}}
			adapter := &RefundAdapter{Client: client}

			resp, err := adapter.RefundPayment(context.Background(), interfaces.RefundProviderRequest{
				GatewayPaymentID: "txn_abc",
				Amount:           decimal.RequireFromString("12.5"),
				Currency:         "usd",
				IdempotencyKey:   "ref_row_1",
			})
			require.NoError(t, err)
			require.Equal(t, "txn_refund_1", resp.GatewayRefundID)
			require.Equal(t, tt.wantStatus, resp.Status)
			require.True(t, resp.SettledAmount.Equal(decimal.RequireFromString(tt.wantSettled)),
				"settled %s, want %s", resp.SettledAmount, tt.wantSettled)

			require.Equal(t, "txn_abc", client.gotTransactionID)
			require.Equal(t, int64(1250), client.gotAmountMinor)
			// Chargebee scopes idempotency keys globally, so the refund row's token
			// must be namespaced per operation.
			require.Equal(t, "ref_row_1:refund", client.gotIdempotencyKey)
		})
	}
}

func TestChargebeeRefundAdapter_RequiresGatewayPaymentID(t *testing.T) {
	adapter := &RefundAdapter{Client: &stubRefundClient{}}
	_, err := adapter.RefundPayment(context.Background(), interfaces.RefundProviderRequest{
		Amount: decimal.NewFromInt(1),
	})
	require.Error(t, err)
}
