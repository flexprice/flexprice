package razorpay

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type stubRefundClient struct {
	RazorpayClient
	gotPaymentID      string
	gotAmountPaise    int64
	gotIdempotencyKey string
	result            map[string]interface{}
	err               error
}

func (c *stubRefundClient) RefundPayment(_ context.Context, paymentID string, amountPaise int64, idempotencyKey string) (map[string]interface{}, error) {
	c.gotPaymentID = paymentID
	c.gotAmountPaise = amountPaise
	c.gotIdempotencyKey = idempotencyKey
	return c.result, c.err
}

func TestRazorpayRefundAdapter_NormalizesStatus(t *testing.T) {
	tests := []struct {
		name        string
		rawStatus   interface{}
		wantStatus  types.RefundStatus
		wantSettled string
	}{
		{"processed settles inline", "processed", types.RefundStatusSucceeded, "12.5"},
		{"pending stays in flight", "pending", types.RefundStatusProcessing, "0"},
		{"failed is terminal", "failed", types.RefundStatusFailed, "0"},
		{"missing status is treated as in flight", nil, types.RefundStatusProcessing, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &stubRefundClient{result: map[string]interface{}{"id": "rfnd_1", "status": tt.rawStatus}}
			adapter := &RefundAdapter{Client: client}

			resp, err := adapter.RefundPayment(context.Background(), interfaces.RefundProviderRequest{
				GatewayPaymentID: "pay_abc",
				Amount:           decimal.RequireFromString("12.5"),
				Currency:         "inr",
				IdempotencyKey:   "ref_row_1",
			})
			require.NoError(t, err)
			require.Equal(t, "rfnd_1", resp.GatewayRefundID)
			require.Equal(t, tt.wantStatus, resp.Status)
			require.True(t, resp.SettledAmount.Equal(decimal.RequireFromString(tt.wantSettled)),
				"settled %s, want %s", resp.SettledAmount, tt.wantSettled)

			require.Equal(t, "pay_abc", client.gotPaymentID)
			require.Equal(t, int64(1250), client.gotAmountPaise)
			// The refund row's own token, not a key derived from the payment: one
			// payment can be refunded more than once.
			require.Equal(t, "ref_row_1", client.gotIdempotencyKey)
		})
	}
}

func TestRazorpayRefundAdapter_RequiresGatewayPaymentID(t *testing.T) {
	adapter := &RefundAdapter{Client: &stubRefundClient{}}
	_, err := adapter.RefundPayment(context.Background(), interfaces.RefundProviderRequest{
		Amount: decimal.NewFromInt(1),
	})
	require.Error(t, err)
}
