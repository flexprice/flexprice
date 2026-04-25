package types

import (
	"regexp"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

var nonAlphanumericRe = regexp.MustCompile(`[^a-z0-9]+`)

type FeatureType string

const (
	FeatureTypeMetered FeatureType = "metered"
	FeatureTypeBoolean FeatureType = "boolean"
	FeatureTypeStatic  FeatureType = "static"
)

func (f FeatureType) String() string {
	return string(f)
}

func (f FeatureType) Validate() error {
	if f == "" {
		return nil
	}

	allowed := []FeatureType{
		FeatureTypeMetered,
		FeatureTypeBoolean,
		FeatureTypeStatic,
	}
	if !lo.Contains(allowed, f) {
		return ierr.NewError("invalid feature type").
			WithHint("Invalid feature type").
			WithReportableDetails(map[string]any{
				"type": f,
				"allowed_types": []string{
					string(FeatureTypeMetered),
					string(FeatureTypeBoolean),
					string(FeatureTypeStatic),
				},
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type FeatureFilter struct {
	*QueryFilter
	*TimeRangeFilter

	*DSLFilter

	// Feature specific filters
	FeatureIDs   []string `form:"feature_ids" json:"feature_ids"`
	MeterIDs     []string `form:"meter_ids" json:"meter_ids"`
	LookupKey    string   `form:"lookup_key" json:"lookup_key"`
	LookupKeys   []string `form:"lookup_keys" json:"lookup_keys"`
	NameContains string   `form:"name_contains" json:"name_contains"`
}

func NewDefaultFeatureFilter() *FeatureFilter {
	return &FeatureFilter{
		QueryFilter: NewDefaultQueryFilter(),
	}
}

func NewNoLimitFeatureFilter() *FeatureFilter {
	return &FeatureFilter{
		QueryFilter: NewNoLimitQueryFilter(),
	}
}

func (f *FeatureFilter) Validate() error {
	if f == nil {
		return nil
	}

	if f.QueryFilter == nil {
		f.QueryFilter = NewDefaultQueryFilter()
	}

	if err := f.QueryFilter.Validate(); err != nil {
		return err
	}

	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}

	if !f.GetExpand().IsEmpty() {
		if err := f.GetExpand().Validate(FeatureExpandConfig); err != nil {
			return err
		}
	}

	if err := f.DSLFilter.Validate(); err != nil {
		return err
	}

	return nil
}

func (f *FeatureFilter) GetLimit() int {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

func (f *FeatureFilter) GetOffset() int {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

func (f *FeatureFilter) GetSort() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

func (f *FeatureFilter) GetStatus() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

func (f *FeatureFilter) GetOrder() string {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetExpand returns the expand filter
func (f *FeatureFilter) GetExpand() Expand {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

func (f *FeatureFilter) IsUnlimited() bool {
	if f == nil || f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}

// FeatureExpandConfig defines the allowed expand fields for features
var FeatureExpandConfig = ExpandConfig{
	AllowedFields: []ExpandableField{ExpandMeters},
	NestedExpands: map[ExpandableField][]ExpandableField{
		ExpandMeters: {},
	},
}

// ReportingUnit defines the display (reporting) unit and conversion from the base feature unit.
// Stored as a single JSON column; API shape: "reporting_unit": { "unit_singular", "unit_plural", "conversion_rate" }.
//
// Formula:
//
//	reporting_unit_value = unit_value × conversion_rate
//
// Example: base unit = "ms", unit_singular = "second", unit_plural = "seconds", conversion_rate = 0.001 → 5000 ms → 5 seconds.
type ReportingUnit struct {
	UnitSingular   string           `json:"unit_singular"`   // Display unit label, singular (e.g. "second")
	UnitPlural     string           `json:"unit_plural"`     // Display unit label, plural (e.g. "seconds")
	ConversionRate *decimal.Decimal `json:"conversion_rate"` // Multiplier: reporting_unit_value = unit_value * conversion_rate; must be > 0
}

// Validate checks that when reporting_unit is provided it has unit_singular, unit_plural, and conversion_rate (all required).
func (r *ReportingUnit) Validate() error {
	if r == nil {
		return nil
	}
	if r.UnitSingular == "" {
		return ierr.NewError("reporting_unit.unit_singular is required").
			WithHint("When providing reporting_unit, unit_singular is required").
			Mark(ierr.ErrValidation)
	}
	if r.UnitPlural == "" {
		return ierr.NewError("reporting_unit.unit_plural is required").
			WithHint("When providing reporting_unit, unit_plural is required").
			Mark(ierr.ErrValidation)
	}
	if r.ConversionRate == nil {
		return ierr.NewError("reporting_unit.conversion_rate is required").
			WithHint("When providing reporting_unit, conversion_rate is required").
			Mark(ierr.ErrValidation)
	}
	if r.ConversionRate.LessThanOrEqual(decimal.Zero) {
		return ierr.NewError("conversion_rate must be positive and non-zero").
			WithHint("Conversion rate must be positive and non-zero").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// generateLookupKey derives a lookup key from a name: lowercase, whitespace/special chars → underscore, trim.
func GenerateLookupKey(name string) string {
	key := strings.ToLower(name)
	key = nonAlphanumericRe.ReplaceAllString(key, "_")
	key = strings.Trim(key, "_")
	return key
}
