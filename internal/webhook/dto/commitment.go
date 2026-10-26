package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalCommitmentEvent struct {
	CommitmentID   string `json:"commitment_id"`
	SubscriptionID string `json:"subscription_id"`
	TenantID       string `json:"tenant_id"`
}

type CommitmentWebhookPayload struct {
	EventType  types.WebhookEventName  `json:"event_type"`
	Commitment *dto.CommitmentResponse `json:"commitment"`
}

func NewCommitmentWebhookPayload(commitment *dto.CommitmentResponse, eventType types.WebhookEventName) *CommitmentWebhookPayload {
	return &CommitmentWebhookPayload{EventType: eventType, Commitment: commitment}
}
