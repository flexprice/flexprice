package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// InternalFXRateEvent is the compact payload published to the system-events topic.
type InternalFXRateEvent struct {
	FXRateID string `json:"fx_rate_id"`
	TenantID string `json:"tenant_id"`
}

// FXRate is the outbound webhook representation of an FX rate.
type FXRate struct {
	ID            string            `json:"id"`
	EnvironmentID string            `json:"environment_id"`
	Scope         string            `json:"scope"`
	ScopeID       string            `json:"scope_id"`
	FromCurrency  string            `json:"from_currency"`
	ToCurrency    string            `json:"to_currency"`
	Rate          string            `json:"rate"`
	ValidFrom     *time.Time        `json:"valid_from,omitempty"`
	ValidTo       *time.Time        `json:"valid_to,omitempty"`
	Status        string            `json:"status"`
	Metadata      map[string]string `json:"metadata,omitempty"`
}

func NewFXRate(resp *dto.FXRateResponse) *FXRate {
	if resp == nil || resp.FXRate == nil {
		return nil
	}
	return &FXRate{
		ID:            resp.ID,
		EnvironmentID: resp.EnvironmentID,
		Scope:         string(resp.Scope),
		ScopeID:       resp.ScopeID,
		FromCurrency:  resp.FromCurrency,
		ToCurrency:    resp.ToCurrency,
		Rate:          resp.Rate.String(),
		ValidFrom:     resp.ValidFrom,
		ValidTo:       resp.ValidTo,
		Status:        string(resp.Status),
		Metadata:      resp.Metadata,
	}
}

type FXRateWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	FXRate    *FXRate                `json:"fx_rate"`
}

func NewFXRateWebhookPayload(rate *dto.FXRateResponse, eventType types.WebhookEventName) *FXRateWebhookPayload {
	return &FXRateWebhookPayload{EventType: eventType, FXRate: NewFXRate(rate)}
}
