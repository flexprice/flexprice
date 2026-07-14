package refund

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestFromEnt_Nil(t *testing.T) {
	assert.Nil(t, FromEnt(nil))
}

func TestFromEnt_MapsFields(t *testing.T) {
	now := time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC)

	e := &ent.Refund{
		ID:                      "refund-1",
		PaymentID:               "payment-1",
		PaymentGateway:          "razorpay",
		GatewayRefundID:         lo.ToPtr("rfnd_abc"),
		GatewayTrackingID:       lo.ToPtr("track_abc"),
		Amount:                  decimal.NewFromInt(100),
		Currency:                "USD",
		RefundStatus:            "PENDING",
		RefundReason:            "REQUESTED_BY_CUSTOMER",
		IdempotencyKey:          "idem-1",
		GatewayIdempotencyToken: "gw-idem-1",
		FailureReason:           nil,
		Metadata:                map[string]string{"k": "v"},
		GatewayMetadata:         map[string]interface{}{"gk": "gv"},
		InitiatedAt:             &now,
		SucceededAt:             nil,
		FailedAt:                nil,
		CancelledAt:             nil,
		EnvironmentID:           "env-1",
	}
	e.TenantID = "tenant-1"
	e.Status = "published"
	e.CreatedAt = now
	e.UpdatedAt = now
	e.CreatedBy = "user-1"
	e.UpdatedBy = "user-1"

	got := FromEnt(e)

	assert.Equal(t, "refund-1", got.ID)
	assert.Equal(t, "payment-1", got.PaymentID)
	assert.Equal(t, "razorpay", got.PaymentGateway)
	assert.Equal(t, "rfnd_abc", *got.GatewayRefundID)
	assert.Equal(t, "track_abc", *got.GatewayTrackingID)
	assert.True(t, decimal.NewFromInt(100).Equal(got.Amount))
	assert.Equal(t, "USD", got.Currency)
	assert.Equal(t, types.RefundStatusPending, got.RefundStatus)
	assert.Equal(t, types.RefundReasonRequestedByCustomer, got.RefundReason)
	assert.Equal(t, "idem-1", got.IdempotencyKey)
	assert.Equal(t, "gw-idem-1", got.GatewayIdempotencyToken)
	assert.Nil(t, got.FailureReason)
	assert.Equal(t, "v", got.Metadata["k"])
	assert.Equal(t, "gv", got.GatewayMetadata["gk"])
	assert.Equal(t, now, *got.InitiatedAt)
	assert.Nil(t, got.SucceededAt)
	assert.Equal(t, "env-1", got.EnvironmentID)
	assert.Equal(t, "tenant-1", got.TenantID)
	assert.Equal(t, types.StatusPublished, got.Status)
}

func TestFromEntList(t *testing.T) {
	assert.Nil(t, FromEntList(nil))

	e1 := &ent.Refund{ID: "r1", RefundStatus: "PENDING", RefundReason: "OTHER"}
	e2 := &ent.Refund{ID: "r2", RefundStatus: "SUCCEEDED", RefundReason: "OTHER"}

	got := FromEntList([]*ent.Refund{e1, e2})
	assert.Len(t, got, 2)
	assert.Equal(t, "r1", got[0].ID)
	assert.Equal(t, "r2", got[1].ID)
}

func TestRefund_TableName(t *testing.T) {
	r := &Refund{}
	assert.Equal(t, "refunds", r.TableName())
}
