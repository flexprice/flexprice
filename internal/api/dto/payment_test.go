package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestPaymentResponse_ToWebhookPayload(t *testing.T) {
	t.Run("returns an untrimmed copy", func(t *testing.T) {
		p := &PaymentResponse{ID: "pay_1", Attempts: []*PaymentAttemptResponse{{ID: "att_1"}}}
		out := p.ToWebhookPayload(types.WebhookEventPaymentSuccess)
		assert.Equal(t, "pay_1", out.ID)
		assert.Len(t, out.Attempts, 1, "PaymentAttemptResponse is small and flat, kept as-is")
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var p *PaymentResponse
		assert.Nil(t, p.ToWebhookPayload(types.WebhookEventPaymentSuccess))
	})
}
