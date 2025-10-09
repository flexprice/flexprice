package payload

import (
	"context"
	"encoding/json"

	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type AlertPayloadBuilder struct {
	services *Services
}

func NewAlertPayloadBuilder(services *Services) PayloadBuilder {
	return &AlertPayloadBuilder{services: services}
}

// BuildPayload for alert webhooks - fetches entities based on what IDs are provided
func (b *AlertPayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	// Unmarshal the internal alert event containing entity IDs (omitempty fields)
	var internalEvent webhookDto.InternalAlertEvent
	if err := json.Unmarshal(data, &internalEvent); err != nil {
		return nil, err
	}

	// Feature alert: needs both feature and wallet
	if internalEvent.FeatureID != "" && internalEvent.WalletID != "" {
		// Fetch feature
		feature, err := b.services.FeatureService.GetFeature(ctx, internalEvent.FeatureID)
		if err != nil {
			return nil, err
		}

		// Fetch wallet
		wallet, err := b.services.WalletService.GetWalletByID(ctx, internalEvent.WalletID)
		if err != nil {
			return nil, err
		}

		// Build the complete alert webhook payload with both entities
		payload := webhookDto.NewAlertWebhookPayload(
			feature,
			wallet,
			internalEvent.AlertType,   // alert_type from internal event
			internalEvent.AlertStatus, // alert_status from internal event
			eventType,
		)

		return json.Marshal(payload)
	}

	// If we get here, no valid combination found - return nil
	return nil, nil
}
