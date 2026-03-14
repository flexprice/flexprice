package feature

import (
	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/group"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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
	ReportingUnit *types.ReportingUnit `json:"reporting_unit,omitempty"`
	AlertSettings *types.AlertSettings `json:"alert_settings,omitempty"`
	GroupID       string               `json:"group_id,omitempty"`
	// Group is populated by the service layer when building responses; repository/FromEnt do not set it.
	Group         *group.Group         `json:"group,omitempty"`
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
	// Check if critical or info threshold is set
	if f.AlertSettings.Critical != nil || f.AlertSettings.Info != nil {
		alertSettings = &f.AlertSettings
	}

	var reportingUnit *types.ReportingUnit
	if f.ReportingUnitSingular != nil && *f.ReportingUnitSingular != "" &&
		f.ReportingUnitPlural != nil && *f.ReportingUnitPlural != "" &&
		f.ReportingUnitConversionRate != nil {
		reportingUnit = &types.ReportingUnit{
			UnitSingular:   *f.ReportingUnitSingular,
			UnitPlural:     *f.ReportingUnitPlural,
			ConversionRate: f.ReportingUnitConversionRate,
		}
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
		ReportingUnit: reportingUnit,
		AlertSettings: alertSettings,
		GroupID:       lo.FromPtr(f.GroupID),
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

// ToReportingValue converts a value from base units to reporting (display) units.
// Formula: unit value = reporting value * conversion_rate.
// Returns error if reporting unit is nil or conversion_rate is not set; otherwise returns the converted value
// rounded to 2 decimal places.
func (r *Feature) ToReportingValue(unitValue decimal.Decimal) (*decimal.Decimal, error) {
	if r == nil || r.ReportingUnit == nil {
		return nil, ierr.NewError("reporting_unit is required").
			WithHint("Feature has no reporting unit; set reporting_unit with conversion_rate to convert values").
			Mark(ierr.ErrValidation)
	}
	if r.ReportingUnit.ConversionRate == nil {
		return nil, ierr.NewError("conversion_rate is required").
			WithHint("Reporting unit must have a conversion_rate to convert unit value to reporting value").
			Mark(ierr.ErrValidation)
	}
	result := unitValue.Div(*r.ReportingUnit.ConversionRate).Round(2)
	return lo.ToPtr(result), nil
}
