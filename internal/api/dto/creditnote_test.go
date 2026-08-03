package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/creditnote"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestCreditNoteResponse_ToWebhookPayload(t *testing.T) {
	newCreditNote := func() *CreditNoteResponse {
		return &CreditNoteResponse{
			CreditNote: &creditnote.CreditNote{ID: "cn_1"},
			Invoice: &InvoiceResponse{
				LineItems: []*InvoiceLineItemResponse{{}},
			},
			Subscription: &SubscriptionResponse{Plan: &PlanResponse{}},
		}
	}

	t.Run("delegates trimming to nested Invoice and Subscription", func(t *testing.T) {
		cn := newCreditNote()
		out := cn.ToWebhookPayload(types.WebhookEventCreditNoteUpdated)
		assert.Nil(t, out.Invoice.LineItems, "InvoiceResponse.ToWebhookPayload drops line items for any event type not finalized/voided")
		assert.Nil(t, out.Subscription.Plan, "SubscriptionResponse.ToWebhookPayload always nils Plan")
	})

	t.Run("does not mutate the receiver", func(t *testing.T) {
		cn := newCreditNote()
		_ = cn.ToWebhookPayload(types.WebhookEventCreditNoteUpdated)
		assert.NotNil(t, cn.Invoice.LineItems, "original credit note's invoice must be untouched")
		assert.NotNil(t, cn.Subscription.Plan)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var cn *CreditNoteResponse
		assert.Nil(t, cn.ToWebhookPayload(types.WebhookEventCreditNoteUpdated))
	})
}
