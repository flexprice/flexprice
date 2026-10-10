package testutil

import (
	"context"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func refundCtx() context.Context {
	return types.SetEnvironmentID(types.SetTenantID(context.Background(), types.DefaultTenantID), "env_test")
}

func newTestRefund(ctx context.Context, id, invoiceID string, paymentID *string, status types.RefundStatus, settled string) *refund.Refund {
	return &refund.Refund{
		ID:                id,
		InvoiceID:         invoiceID,
		PaymentID:         paymentID,
		Amount:            decimal.RequireFromString(settled),
		SettledAmount:     decimal.RequireFromString(settled),
		Currency:          "usd",
		RefundStatus:      status,
		RefundReason:      types.RefundReasonOther,
		RefundDestination: types.RefundDestinationWallet,
		IdempotencyKey:    id,
		EnvironmentID:     types.GetEnvironmentID(ctx),
		BaseModel:         types.GetDefaultBaseModel(ctx),
	}
}

func TestInMemoryRefundStore_SumSettled_IgnoresUnsettledRows(t *testing.T) {
	ctx := refundCtx()
	store := NewInMemoryRefundStore()

	pay1, pay2 := "pay_1", "pay_2"
	require.NoError(t, store.CreateBulk(ctx, []*refund.Refund{
		newTestRefund(ctx, "ref_1", "inv_1", &pay1, types.RefundStatusSucceeded, "10"),
		newTestRefund(ctx, "ref_2", "inv_1", &pay1, types.RefundStatusSucceeded, "5"),
		newTestRefund(ctx, "ref_3", "inv_1", &pay2, types.RefundStatusProcessing, "7"),
		newTestRefund(ctx, "ref_4", "inv_1", nil, types.RefundStatusSucceeded, "3"),
		newTestRefund(ctx, "ref_5", "inv_2", &pay1, types.RefundStatusSucceeded, "100"),
	}))

	byPayment, err := store.SumSettledByPaymentIDs(ctx, "inv_1", []string{pay1, pay2})
	require.NoError(t, err)
	require.True(t, byPayment[pay1].Equal(decimal.RequireFromString("15")), "the same payment's refunds on another invoice must not count")
	_, ok := byPayment[pay2]
	require.False(t, ok, "processing refund must not contribute settled cash")

	inFlight, err := store.SumInFlightByPaymentIDs(ctx, "inv_1", []string{pay1, pay2})
	require.NoError(t, err)
	require.True(t, inFlight[pay2].Equal(decimal.RequireFromString("7")))
	_, ok = inFlight[pay1]
	require.False(t, ok, "settled refunds are not in flight")

	empty, err := store.SumSettledByPaymentIDs(ctx, "inv_1", nil)
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestInMemoryRefundStore_ListByInvoice_ExcludesArchived(t *testing.T) {
	ctx := refundCtx()
	store := NewInMemoryRefundStore()

	require.NoError(t, store.CreateBulk(ctx, []*refund.Refund{
		newTestRefund(ctx, "ref_1", "inv_1", nil, types.RefundStatusSucceeded, "10"),
		newTestRefund(ctx, "ref_2", "inv_2", nil, types.RefundStatusSucceeded, "10"),
	}))
	require.NoError(t, store.Delete(ctx, "ref_1"))

	refunds, err := store.ListByInvoice(ctx, "inv_1")
	require.NoError(t, err)
	require.Empty(t, refunds)

	refunds, err = store.ListByInvoice(ctx, "inv_2")
	require.NoError(t, err)
	require.Len(t, refunds, 1)
}

func TestInMemoryRefundStore_GetByGatewayRefundID(t *testing.T) {
	ctx := refundCtx()
	store := NewInMemoryRefundStore()

	gateway, gatewayRefundID := "razorpay", "rfnd_abc"
	r := newTestRefund(ctx, "ref_1", "inv_1", nil, types.RefundStatusProcessing, "10")
	r.PaymentGateway = &gateway
	r.GatewayRefundID = &gatewayRefundID
	require.NoError(t, store.Create(ctx, r))

	got, err := store.GetByGatewayRefundID(ctx, gateway, gatewayRefundID)
	require.NoError(t, err)
	require.Equal(t, "ref_1", got.ID)

	_, err = store.GetByGatewayRefundID(ctx, "stripe", gatewayRefundID)
	require.Error(t, err)
}

func TestInMemoryRefundStore_SumSettledToWalletByInvoice(t *testing.T) {
	ctx := refundCtx()
	store := NewInMemoryRefundStore()

	gateway := newTestRefund(ctx, "ref_gw", "inv_1", nil, types.RefundStatusSucceeded, "50")
	gateway.RefundDestination = types.RefundDestinationGateway
	otherCurrency := newTestRefund(ctx, "ref_inr", "inv_1", nil, types.RefundStatusSucceeded, "40")
	otherCurrency.Currency = "inr"
	require.NoError(t, store.CreateBulk(ctx, []*refund.Refund{
		newTestRefund(ctx, "ref_1", "inv_1", nil, types.RefundStatusSucceeded, "10"),
		newTestRefund(ctx, "ref_2", "inv_1", nil, types.RefundStatusSucceeded, "5"),
		newTestRefund(ctx, "ref_3", "inv_1", nil, types.RefundStatusPending, "7"),
		newTestRefund(ctx, "ref_4", "inv_2", nil, types.RefundStatusSucceeded, "100"),
		gateway,
		otherCurrency,
	}))

	got, err := store.SumSettledToWalletByInvoice(ctx, "inv_1", "USD")
	require.NoError(t, err)
	require.True(t, got.Equal(decimal.RequireFromString("15")), "only settled wallet rows of this invoice in usd, got %s", got)

	none, err := store.SumSettledToWalletByInvoice(ctx, "inv_9", "usd")
	require.NoError(t, err)
	require.True(t, none.IsZero())
}
