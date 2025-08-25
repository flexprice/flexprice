package payload

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/alert"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

// AlertPayloadBuilder builds webhook payloads for alert events
type AlertPayloadBuilder struct {
	services *Services
}

// NewAlertPayloadBuilder creates a new alert payload builder
func NewAlertPayloadBuilder(services *Services) PayloadBuilder {
	return &AlertPayloadBuilder{
		services: services,
	}
}

// BuildPayload builds the webhook payload for alert events
func (b *AlertPayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	// Parse the internal event
	var internalEvent webhookDto.InternalAlertEvent
	if err := json.Unmarshal(data, &internalEvent); err != nil {
		return nil, err
	}

	// Convert internal event to alert response
	alertResp := dto.NewAlertResponse(&alert.Alert{
		ID:            internalEvent.AlertID,
		EntityType:    internalEvent.EntityType,
		EntityID:      internalEvent.EntityID,
		AlertMetric:   internalEvent.AlertMetric,
		AlertState:    types.AlertState(internalEvent.AlertState),
		AlertEnabled:  internalEvent.AlertEnabled,
		AlertData:     internalEvent.AlertData,
		EnvironmentID: internalEvent.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID: internalEvent.TenantID,
		},
	})

	// Build webhook payload using existing constructor
	payload := webhookDto.NewAlertWebhookPayload(alertResp)

	return json.Marshal(payload)
}
