package payload

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

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

type FeaturePayloadBuilder struct {
	services *Services
}

func NewFeaturePayloadBuilder(services *Services) PayloadBuilder {
	return &FeaturePayloadBuilder{services: services}
}

func (b *FeaturePayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
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

	payload := NewFeatureWebhookPayload(feature, eventType)

	return json.Marshal(payload)
}
