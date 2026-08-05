package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalFeatureEvent struct {
	FeatureID string `json:"feature_id"`
	TenantID  string `json:"tenant_id"`
}

type Feature struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	LookupKey    string            `json:"lookup_key"`
	Description  string            `json:"description,omitempty"`
	Type         types.FeatureType `json:"type"`
	UnitSingular string            `json:"unit_singular,omitempty"`
	UnitPlural   string            `json:"unit_plural,omitempty"`
	MeterID      string            `json:"meter_id,omitempty"`
	GroupID      string            `json:"group_id,omitempty"`
	Metadata     types.Metadata    `json:"metadata,omitempty"`
}

func NewFeature(resp *dto.FeatureResponse) *Feature {
	if resp == nil || resp.Feature == nil {
		return nil
	}
	return &Feature{
		ID:           resp.ID,
		Name:         resp.Name,
		LookupKey:    resp.LookupKey,
		Description:  resp.Description,
		Type:         resp.Type,
		UnitSingular: resp.UnitSingular,
		UnitPlural:   resp.UnitPlural,
		MeterID:      resp.MeterID,
		GroupID:      resp.GroupID,
		Metadata:     resp.Metadata,
	}
}

type FeatureWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Feature   *Feature               `json:"feature"`
}

func NewFeatureWebhookPayload(feature *dto.FeatureResponse, eventType types.WebhookEventName) *FeatureWebhookPayload {
	return &FeatureWebhookPayload{EventType: eventType, Feature: NewFeature(feature)}
}
