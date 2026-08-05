package outbound

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
)

// SubscriptionPhaseWebhookPayload is the minimal webhook representation of a subscription phase.
type SubscriptionPhaseWebhookPayload struct {
	ID             string     `json:"id"`
	SubscriptionID string     `json:"subscription_id"`
	StartDate      time.Time  `json:"start_date"`
	EndDate        *time.Time `json:"end_date,omitempty"`
}

func NewSubscriptionPhaseWebhookPayload(resp *dto.SubscriptionPhaseResponse) *SubscriptionPhaseWebhookPayload {
	if resp == nil || resp.SubscriptionPhase == nil {
		return nil
	}
	return &SubscriptionPhaseWebhookPayload{
		ID:             resp.ID,
		SubscriptionID: resp.SubscriptionID,
		StartDate:      resp.StartDate,
		EndDate:        resp.EndDate,
	}
}
