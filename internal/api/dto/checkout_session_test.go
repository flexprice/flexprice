package dto

import (
	"testing"

	domainCheckout "github.com/flexprice/flexprice/internal/domain/checkout"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestCheckoutSessionResponse_ToWebhookPayload(t *testing.T) {
	t.Run("returns an untrimmed copy", func(t *testing.T) {
		s := &CheckoutSessionResponse{CheckoutSession: &domainCheckout.CheckoutSession{ID: "cs_1"}}
		out := s.ToWebhookPayload(types.WebhookEventCheckoutSessionCompleted)
		assert.Equal(t, "cs_1", out.ID)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var s *CheckoutSessionResponse
		assert.Nil(t, s.ToWebhookPayload(types.WebhookEventCheckoutSessionCompleted))
	})
}
