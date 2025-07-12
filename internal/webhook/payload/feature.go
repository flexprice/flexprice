package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type FeaturePayloadBuilder struct {
	services *Services
}

func NewFeaturePayloadBuilder(services *Services) PayloadBuilder {
	return &FeaturePayloadBuilder{services: services}
}

func (b *FeaturePayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalFeatureEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal feature event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	featureID, tenantID := parsedPayload.FeatureID, parsedPayload.TenantID
	if featureID == "" || tenantID == "" {
		return nil, ierr.NewError("invalid data type for feature event").
			WithHint("Please provide a valid feature ID and tenant ID").
			WithReportableDetails(map[string]any{
				"feature_id": featureID,
				"tenant_id":  tenantID,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	feature, err := b.services.FeatureService.GetFeature(ctx, featureID)
	if err != nil {
		return nil, err
	}

	payload := webhookDto.NewFeatureWebhookPayload(feature)

	return json.Marshal(payload)
}
