package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/refund"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRefund(id string) *refund.Refund {
	now := time.Now().UTC()
	return &refund.Refund{
		ID:                      id,
		PaymentID:               "payment-1",
		PaymentGateway:          "razorpay",
		Amount:                  decimal.NewFromInt(100),
		Currency:                "USD",
		RefundStatus:            types.RefundStatusPending,
		RefundReason:            types.RefundReasonRequestedByCustomer,
		IdempotencyKey:          "idem-" + id,
		GatewayIdempotencyToken: "gw-idem-" + id,
		EnvironmentID:           "test-env",
		BaseModel: types.BaseModel{
			TenantID:  "test-tenant",
			Status:    types.StatusPublished,
			CreatedAt: now,
			UpdatedAt: now,
		},
	}
}

func TestInMemoryRefundStore_Create(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryRefundStore()

	t.Run("successful creation", func(t *testing.T) {
		r := newTestRefund("refund-1")
		err := store.Create(ctx, r)
		require.NoError(t, err)

		got, err := store.Get(ctx, "refund-1")
		require.NoError(t, err)
		assert.Equal(t, r.PaymentID, got.PaymentID)
		assert.Equal(t, r.RefundStatus, got.RefundStatus)
	})

	t.Run("nil refund", func(t *testing.T) {
		err := store.Create(ctx, nil)
		assert.Error(t, err)
	})

	t.Run("empty ID", func(t *testing.T) {
		r := newTestRefund("")
		err := store.Create(ctx, r)
		assert.Error(t, err)
	})
}

func TestInMemoryRefundStore_Get_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryRefundStore()

	_, err := store.Get(ctx, "does-not-exist")
	assert.Error(t, err)
}

func TestInMemoryRefundStore_Update(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryRefundStore()

	r := newTestRefund("refund-2")
	require.NoError(t, store.Create(ctx, r))

	r.RefundStatus = types.RefundStatusSucceeded
	require.NoError(t, store.Update(ctx, r))

	got, err := store.Get(ctx, "refund-2")
	require.NoError(t, err)
	assert.Equal(t, types.RefundStatusSucceeded, got.RefundStatus)
}

func TestInMemoryRefundStore_Delete(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryRefundStore()

	r := newTestRefund("refund-3")
	require.NoError(t, store.Create(ctx, r))
	require.NoError(t, store.Delete(ctx, "refund-3"))

	_, err := store.Get(ctx, "refund-3")
	assert.Error(t, err)
}

func TestInMemoryRefundStore_GetByIdempotencyKey(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryRefundStore()

	r := newTestRefund("refund-4")
	require.NoError(t, store.Create(ctx, r))

	got, err := store.GetByIdempotencyKey(ctx, "idem-refund-4")
	require.NoError(t, err)
	assert.Equal(t, "refund-4", got.ID)

	_, err = store.GetByIdempotencyKey(ctx, "does-not-exist")
	assert.Error(t, err)
}

func TestInMemoryRefundStore_ListAndCount(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryRefundStore()

	require.NoError(t, store.Create(ctx, newTestRefund("refund-5")))
	require.NoError(t, store.Create(ctx, newTestRefund("refund-6")))

	list, err := store.List(ctx, &types.RefundFilter{QueryFilter: types.NewNoLimitQueryFilter()})
	require.NoError(t, err)
	assert.Len(t, list, 2)

	count, err := store.Count(ctx, &types.RefundFilter{QueryFilter: types.NewNoLimitQueryFilter()})
	require.NoError(t, err)
	assert.Equal(t, 2, count)

	paymentID := "payment-1"
	filtered, err := store.List(ctx, &types.RefundFilter{
		PaymentID:   &paymentID,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	require.NoError(t, err)
	assert.Len(t, filtered, 2)

	other := "no-such-payment"
	empty, err := store.List(ctx, &types.RefundFilter{
		PaymentID:   &other,
		QueryFilter: types.NewNoLimitQueryFilter(),
	})
	require.NoError(t, err)
	assert.Len(t, empty, 0)
}
