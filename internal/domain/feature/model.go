package feature

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type Feature struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	LookupKey     string               `json:"lookup_key"`
	Description   string               `json:"description"`
	MeterID       string               `json:"meter_id"`
	Metadata      types.Metadata       `json:"metadata"`
	Type          types.FeatureType    `json:"type"`
	UnitSingular  string               `json:"unit_singular"`
	UnitPlural    string               `json:"unit_plural"`
	AlertSettings *types.AlertSettings `json:"alert_settings,omitempty"`
	EnvironmentID string               `json:"environment_id"`
	types.BaseModel
}

// FromEnt converts ent.Feature to domain Feature
func FromEnt(f *ent.Feature) *Feature {
	if f == nil {
		return nil
	}

	// Extract alert settings from Ent entity
	var alertSettings *types.AlertSettings
	// Check if alert settings are set (critical threshold must be provided)
	if f.AlertSettings.Critical != nil {
		alertSettings = &f.AlertSettings
	}

	return &Feature{
		ID:            f.ID,
		Name:          f.Name,
		LookupKey:     f.LookupKey,
		Description:   lo.FromPtr(f.Description),
		MeterID:       lo.FromPtr(f.MeterID),
		Metadata:      types.Metadata(f.Metadata),
		Type:          types.FeatureType(f.Type),
		UnitSingular:  lo.FromPtr(f.UnitSingular),
		UnitPlural:    lo.FromPtr(f.UnitPlural),
		AlertSettings: alertSettings,
		EnvironmentID: f.EnvironmentID,
		BaseModel: types.BaseModel{
			TenantID:  f.TenantID,
			Status:    types.Status(f.Status),
			CreatedAt: f.CreatedAt,
			UpdatedAt: f.UpdatedAt,
			CreatedBy: f.CreatedBy,
			UpdatedBy: f.UpdatedBy,
		},
	}
}

// FromEntList converts []*ent.Feature to []*Feature
func FromEntList(features []*ent.Feature) []*Feature {
	result := make([]*Feature, len(features))
	for i, f := range features {
		result[i] = FromEnt(f)
	}
	return result
}
