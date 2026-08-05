package outbound

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// FeatureWebhookPayload is the minimal webhook representation of a feature.
type FeatureWebhookPayload struct {
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

func NewFeatureWebhookPayload(resp *dto.FeatureResponse) *FeatureWebhookPayload {
	if resp == nil || resp.Feature == nil {
		return nil
	}
	return &FeatureWebhookPayload{
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
