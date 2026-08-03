package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestGroupResponse_ToWebhookPayload(t *testing.T) {
	t.Run("returns an untrimmed copy", func(t *testing.T) {
		g := &GroupResponse{ID: "grp_1", Name: "Group 1"}
		out := g.ToWebhookPayload(types.WebhookEventSubscriptionGroupSpendThresholdReached)
		assert.Equal(t, "grp_1", out.ID)
		assert.Equal(t, "Group 1", out.Name)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var g *GroupResponse
		assert.Nil(t, g.ToWebhookPayload(types.WebhookEventSubscriptionGroupSpendThresholdReached))
	})
}
