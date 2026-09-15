package analytics

import (
	"regexp"
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// validPropertyField matches the charset allowed after "properties.".
var validPropertyField = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)

// ValidateDimensions checks each dimension against the meter_usage engine's
// group_by allowlist: meter_id, source, external_customer_id, customer_id
// (analytics-facing alias), or properties.<field>.
func ValidateDimensions(dims []string) error {
	for _, d := range dims {
		if err := validateDimension(d); err != nil {
			return err
		}
	}
	return nil
}

func validateDimension(d string) error {
	switch d {
	case "meter_id", "source", "external_customer_id", "customer_id":
		return nil
	}
	if field, ok := strings.CutPrefix(d, "properties."); ok && field != "" && validPropertyField.MatchString(field) {
		return nil
	}
	return ierr.NewErrorf("illegal dimension: %q", d).
		WithHint("dimensions must be one of meter_id, source, external_customer_id, customer_id, or properties.<field>").
		Mark(ierr.ErrValidation)
}

// Filter is a single filter clause of a ViewDefinition.
type Filter struct {
	Field    string                   `json:"field"`
	Op       types.FilterOperatorType `json:"op" enums:"eq,in"`
	Value    []string                 `json:"value"`
	Optional bool                     `json:"optional,omitempty"`
}

type SortSpec struct {
	Field string              `json:"field"`
	Dir   types.SortDirection `json:"dir"`
}

type Variable struct {
	Name     string             `json:"name"`
	Type     types.VariableType `json:"type" binding:"required" validate:"required" enums:"date_range,string,string_list,number,enum,boolean"`
	Required bool               `json:"required,omitempty"`
	Default  []string           `json:"default,omitempty"`
}

// TimeSpecRaw is the unresolved time spec. Range is a single string: a
// "{{var}}" placeholder, a relative range token (see resolveTime —
// "last_N_days", "last_N_hours", "today", "yesterday"), an absolute
// "<from>..<to>" range ("YYYY-MM-DD" dates), or omitted entirely (defaults
// to "last_7_days").
type TimeSpecRaw struct {
	Range string      `json:"range,omitempty"`
	Grain types.Grain `json:"grain"`
}

type TimeSpec struct {
	From  time.Time
	To    time.Time
	Grain types.Grain
}

type ViewDefinition struct {
	Name       string         `json:"name"`
	Shape      types.Shape    `json:"shape" binding:"required" validate:"required" enums:"timeseries,breakdown"`
	Metrics    []types.Metric `json:"metrics" binding:"required,min=1" validate:"required,min=1" enums:"usage_quantity,event_count"`
	Dimensions []string       `json:"dimensions,omitempty"`
	Filters    []*Filter      `json:"filters,omitempty"`
	// Time is optional: an omitted range defaults to last_7_days (see
	// resolveTime), so it is intentionally not marked required.
	Time      TimeSpecRaw `json:"time"`
	Sort      []*SortSpec `json:"sort,omitempty"`
	Limit     int         `json:"limit,omitempty"`
	Variables []*Variable `json:"variables,omitempty"`
}

// NewViewDefinition builds a ViewDefinition from its constituent parts.
func NewViewDefinition(
	name string,
	shape types.Shape,
	metrics []types.Metric,
	dimensions []string,
	filters []*Filter,
	timeSpec TimeSpecRaw,
	sort []*SortSpec,
	limit int,
	variables []*Variable,
) ViewDefinition {
	return ViewDefinition{
		Name:       name,
		Shape:      shape,
		Metrics:    metrics,
		Dimensions: dimensions,
		Filters:    filters,
		Time:       timeSpec,
		Sort:       sort,
		Limit:      limit,
		Variables:  variables,
	}
}

func (v ViewDefinition) Validate() error {
	switch v.Shape {
	case types.ShapeTimeseries, types.ShapeBreakdown:
	default:
		return ierr.NewError("unsupported shape").
			WithHint("shape must be one of: timeseries, breakdown").
			Mark(ierr.ErrValidation)
	}
	if len(v.Metrics) == 0 {
		return ierr.NewError("at least one metric is required").Mark(ierr.ErrValidation)
	}
	for _, m := range v.Metrics {
		if err := m.Validate(); err != nil {
			return err
		}
	}
	if err := ValidateDimensions(v.Dimensions); err != nil {
		return err
	}
	if err := v.Time.Grain.Validate(); err != nil {
		return err
	}
	if v.Shape == types.ShapeTimeseries && (len(v.Sort) > 0 || v.Limit > 0) {
		return ierr.NewError("sort and limit are not supported for timeseries views").
			WithHint("timeseries rows are time-ordered per bucket; series-level ranking is not yet supported — remove sort/limit or use a breakdown view").
			Mark(ierr.ErrValidation)
	}
	for _, s := range v.Sort {
		if s == nil {
			continue
		}
		switch s.Dir {
		case "", types.SortDirectionAsc, types.SortDirectionDesc:
		default:
			return ierr.NewErrorf("invalid sort direction %q", s.Dir).
				WithHint("dir must be one of: asc, desc (empty defaults to asc)").
				Mark(ierr.ErrValidation)
		}
	}
	for _, f := range v.Filters {
		if f == nil {
			continue
		}
		switch f.Op {
		case types.EQUAL, types.IN:
		default:
			return ierr.NewErrorf("unsupported filter operator %q", f.Op).
				WithHint("filter operator must be one of: eq, in").
				Mark(ierr.ErrValidation)
		}
	}
	for _, va := range v.Variables {
		if va == nil {
			continue
		}
		if err := va.Type.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// ResolvedView is a ViewDefinition with all variables resolved to concrete
// values — built by ResolveVariables.
type ResolvedView struct {
	Shape      types.Shape
	Metrics    []types.Metric
	Dimensions []string
	Filters    []*Filter // concrete values; optional-with-unsupplied dropped
	Time       TimeSpec
	Sort       []*SortSpec
	Limit      int
}
