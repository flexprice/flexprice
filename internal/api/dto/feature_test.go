package dto

import (
	"testing"

	"github.com/flexprice/flexprice/internal/domain/feature"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestFeatureResponse_ToWebhookPayload(t *testing.T) {
	t.Run("delegates trimming to nested Group, keeps Meter", func(t *testing.T) {
		f := &FeatureResponse{
			Feature: &feature.Feature{ID: "feat_1"},
			Meter:   &MeterResponse{ID: "meter_1"},
			Group:   &GroupResponse{ID: "grp_1"},
		}
		out := f.ToWebhookPayload(types.WebhookEventFeatureUpdated)
		assert.NotNil(t, out.Meter, "meter is small and flat, kept as-is")
		assert.Equal(t, "grp_1", out.Group.ID)
	})

	t.Run("nil Group passes through as nil", func(t *testing.T) {
		f := &FeatureResponse{Feature: &feature.Feature{ID: "feat_1"}}
		out := f.ToWebhookPayload(types.WebhookEventFeatureUpdated)
		assert.Nil(t, out.Group)
	})

	t.Run("nil receiver returns nil", func(t *testing.T) {
		var f *FeatureResponse
		assert.Nil(t, f.ToWebhookPayload(types.WebhookEventFeatureUpdated))
	})
}
