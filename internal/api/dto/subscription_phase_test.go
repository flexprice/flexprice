package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestSubscriptionPhaseResponse_ToWebhookPayload(t *testing.T) {
	t.Run("returns an untrimmed copy", func(t *testing.T) {
		p := &SubscriptionPhaseResponse{SubscriptionPhase: &subscription.SubscriptionPhase{ID: "phase_1"}}
		out := p.ToWebhookPayload(types.WebhookEventSubscriptionPhaseUpdated)
		assert.Equal(t, "phase_1", out.ID)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var p *SubscriptionPhaseResponse
		assert.Nil(t, p.ToWebhookPayload(types.WebhookEventSubscriptionPhaseUpdated))
	})
}
