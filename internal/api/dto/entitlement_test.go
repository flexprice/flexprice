package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/entitlement"
	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestEntitlementResponse_ToWebhookPayload(t *testing.T) {
	newEntitlement := func() *EntitlementResponse {
		return &EntitlementResponse{
			Entitlement: &entitlement.Entitlement{ID: "ent_1"},
			Feature:     &FeatureResponse{Feature: &feature.Feature{ID: "feat_1"}, Group: &GroupResponse{ID: "grp_1"}},
			Plan:        &PlanResponse{},
			Addon:       &AddonResponse{},
		}
	}

	t.Run("nils Plan and Addon, delegates trimming to Feature", func(t *testing.T) {
		ent := newEntitlement()
		out := ent.ToWebhookPayload(types.WebhookEventEntitlementUpdated)
		assert.Nil(t, out.Plan)
		assert.Nil(t, out.Addon)
		assert.NotNil(t, out.Feature)
		assert.Equal(t, "grp_1", out.Feature.Group.ID, "Feature.ToWebhookPayload keeps Group as-is")
	})

	t.Run("does not mutate the receiver", func(t *testing.T) {
		ent := newEntitlement()
		_ = ent.ToWebhookPayload(types.WebhookEventEntitlementUpdated)
		assert.NotNil(t, ent.Plan, "original entitlement must be untouched")
		assert.NotNil(t, ent.Addon)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var ent *EntitlementResponse
		assert.Nil(t, ent.ToWebhookPayload(types.WebhookEventEntitlementUpdated))
	})
}
