package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCreateInvoiceRequest_ZeroOutAmounts(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(99),
		Total:     decimal.NewFromInt(99),
		AmountDue: decimal.NewFromInt(99),
		LineItems: []CreateInvoiceLineItemRequest{
			{Amount: decimal.NewFromInt(99), Quantity: decimal.NewFromInt(2)},
			{Amount: decimal.NewFromInt(49), Quantity: decimal.NewFromInt(1)},
		},
	}

	req.ZeroOutAmounts()

	assert.True(t, req.Subtotal.IsZero(), "Subtotal must be zero")
	assert.True(t, req.Total.IsZero(), "Total must be zero")
	assert.True(t, req.AmountDue.IsZero(), "AmountDue must be zero")

	for i, li := range req.LineItems {
		assert.True(t, li.Amount.IsZero(), "line item %d Amount must be zero", i)
		// Quantity is deliberately preserved — it shows the pricing skeleton.
		assert.False(t, li.Quantity.IsZero(), "line item %d Quantity must be preserved", i)
	}
}

func TestCreateInvoiceRequest_ZeroOutAmounts_EmptyLineItems(t *testing.T) {
	req := CreateInvoiceRequest{
		Subtotal:  decimal.NewFromInt(50),
		Total:     decimal.NewFromInt(50),
		AmountDue: decimal.NewFromInt(50),
	}
	req.ZeroOutAmounts() // must not panic on nil/empty LineItems
	assert.True(t, req.Subtotal.IsZero())
	assert.True(t, req.Total.IsZero())
	assert.True(t, req.AmountDue.IsZero())
}

func TestInvoiceResponse_ToWebhookPayload(t *testing.T) {
	newInvoice := func() *InvoiceResponse {
		return &InvoiceResponse{
			LineItems:    []*InvoiceLineItemResponse{{}},
			Subscription: &SubscriptionResponse{Plan: &PlanResponse{}},
		}
	}

	t.Run("finalized keeps line items", func(t *testing.T) {
		inv := newInvoice()
		out := inv.ToWebhookPayload(types.WebhookEventInvoiceUpdateFinalized)
		assert.NotNil(t, out.LineItems)
		assert.Len(t, out.LineItems, 1)
	})

	t.Run("voided keeps line items", func(t *testing.T) {
		inv := newInvoice()
		out := inv.ToWebhookPayload(types.WebhookEventInvoiceUpdateVoided)
		assert.NotNil(t, out.LineItems)
	})

	t.Run("payment update drops line items", func(t *testing.T) {
		inv := newInvoice()
		out := inv.ToWebhookPayload(types.WebhookEventInvoiceUpdatePayment)
		assert.Nil(t, out.LineItems)
	})

	t.Run("generic update drops line items", func(t *testing.T) {
		inv := newInvoice()
		out := inv.ToWebhookPayload(types.WebhookEventInvoiceUpdate)
		assert.Nil(t, out.LineItems)
	})

	t.Run("payment overdue drops line items", func(t *testing.T) {
		inv := newInvoice()
		out := inv.ToWebhookPayload(types.WebhookEventInvoicePaymentOverdue)
		assert.Nil(t, out.LineItems)
	})

	t.Run("communication triggered drops line items", func(t *testing.T) {
		inv := newInvoice()
		out := inv.ToWebhookPayload(types.WebhookEventInvoiceCommunicationTriggered)
		assert.Nil(t, out.LineItems)
	})

	t.Run("nested subscription is trimmed via delegation", func(t *testing.T) {
		inv := newInvoice()
		out := inv.ToWebhookPayload(types.WebhookEventInvoiceUpdateFinalized)
		assert.NotNil(t, out.Subscription)
		assert.Nil(t, out.Subscription.Plan)
	})

	t.Run("does not mutate the receiver", func(t *testing.T) {
		inv := newInvoice()
		_ = inv.ToWebhookPayload(types.WebhookEventInvoiceUpdatePayment)
		assert.NotNil(t, inv.LineItems, "original invoice must be untouched")
		assert.NotNil(t, inv.Subscription.Plan, "original subscription must be untouched")
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var inv *InvoiceResponse
		assert.Nil(t, inv.ToWebhookPayload(types.WebhookEventInvoiceUpdateFinalized))
	})
}
