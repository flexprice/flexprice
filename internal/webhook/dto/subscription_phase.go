package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalSubscriptionPhaseEvent struct {
	PhaseID  string `json:"phase_id"`
	TenantID string `json:"tenant_id"`
}

type SubscriptionPhase struct {
	ID             string     `json:"id"`
	SubscriptionID string     `json:"subscription_id"`
	StartDate      time.Time  `json:"start_date"`
	EndDate        *time.Time `json:"end_date,omitempty"`
}

func NewSubscriptionPhase(resp *dto.SubscriptionPhaseResponse) *SubscriptionPhase {
	if resp == nil || resp.SubscriptionPhase == nil {
		return nil
	}
	return &SubscriptionPhase{
		ID:             resp.ID,
		SubscriptionID: resp.SubscriptionID,
		StartDate:      resp.StartDate,
		EndDate:        resp.EndDate,
	}
}

type SubscriptionPhaseWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Phase     *SubscriptionPhase     `json:"phase"`
}

func NewSubscriptionPhaseWebhookPayload(phase *SubscriptionPhase, eventType types.WebhookEventName) *SubscriptionPhaseWebhookPayload {
	return &SubscriptionPhaseWebhookPayload{EventType: eventType, Phase: phase}
}
