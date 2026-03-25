package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalEntitlementEvent struct {
	EntitlementID string `json:"entitlement_id"`
	TenantID      string `json:"tenant_id"`
}

type EntitlementWebhookPayload struct {
	EventType   types.WebhookEventName   `json:"event_type"`
	Entitlement *dto.EntitlementResponse `json:"entitlement"`
}

func NewEntitlementWebhookPayload(entitlement *dto.EntitlementResponse, eventType types.WebhookEventName) *EntitlementWebhookPayload {
	return &EntitlementWebhookPayload{EventType: eventType, Entitlement: entitlement}
}
