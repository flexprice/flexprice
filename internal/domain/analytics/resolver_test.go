package analytics

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveVariables_DropsUnsuppliedOptionalFilter(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*Filter{
			{Field: "meter_id", Op: types.EQUAL, Value: []string{"{{meter}}"}},
			{Field: "customer_id", Op: types.IN, Value: []string{"{{customers}}"}, Optional: true},
		},
		Time:      TimeSpecRaw{Range: "{{date_range}}", Grain: types.GrainDay},
		Variables: []*Variable{{Name: "meter", Type: types.VariableTypeString, Required: true}, {Name: "customers", Type: types.VariableTypeStringList}},
	}
	rv, err := ResolveVariables(&def, map[string][]string{
		"meter":      {"meter_1"},
		"date_range": {"2026-08-01..2026-09-01"},
	})
	require.NoError(t, err)
	assert.Len(t, rv.Filters, 1) // optional customers filter dropped
	assert.Equal(t, []string{"meter_1"}, rv.Filters[0].Value)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), rv.Time.From)
}

func TestResolveVariables_MissingRequiredVar(t *testing.T) {
	def := ViewDefinition{
		Shape: types.ShapeTimeseries, Metrics: []types.Metric{types.MetricUsageQuantity},
		Variables: []*Variable{{Name: "meter", Type: types.VariableTypeString, Required: true}},
	}
	_, err := ResolveVariables(&def, map[string][]string{})
	require.Error(t, err)
}

func TestValidate_RejectsUnknownShape(t *testing.T) {
	def := ViewDefinition{Shape: types.Shape("pie"), Metrics: []types.Metric{types.MetricUsageQuantity}}
	require.Error(t, def.Validate())
}

func TestResolveVariables_MissingRequiredFilterValue(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*Filter{
			{Field: "meter_id", Op: types.EQUAL, Value: []string{"{{meter}}"}},
		},
		Time: TimeSpecRaw{Range: "2026-08-01..2026-09-01", Grain: types.GrainDay},
	}
	_, err := ResolveVariables(&def, map[string][]string{})
	require.Error(t, err)
}

func TestResolveVariables_LiteralFilterValuePassesThrough(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*Filter{
			{Field: "status", Op: types.EQUAL, Value: []string{"active"}},
		},
		Time: TimeSpecRaw{Range: "2026-08-01..2026-09-01", Grain: types.GrainDay},
	}
	rv, err := ResolveVariables(&def, map[string][]string{})
	require.NoError(t, err)
	require.Len(t, rv.Filters, 1)
	assert.Equal(t, []string{"active"}, rv.Filters[0].Value)
}

func TestResolveVariables_TimeRangeNotSupplied(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Range: "{{date_range}}", Grain: types.GrainDay},
	}
	_, err := ResolveVariables(&def, map[string][]string{})
	require.Error(t, err)
}

func TestResolveVariables_TimeRangeEmptyVariable(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Range: "{{date_range}}", Grain: types.GrainDay},
	}
	_, err := ResolveVariables(&def, map[string][]string{"date_range": {}})
	require.Error(t, err)
}

func TestResolveVariables_InvalidDateFormat(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Range: "{{date_range}}", Grain: types.GrainDay},
	}
	_, err := ResolveVariables(&def, map[string][]string{
		"date_range": {"08/01/2026..2026-09-01"},
	})
	require.Error(t, err)
}

func TestResolveVariables_LiteralTimeRange(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Range: "2026-08-01..2026-09-01", Grain: types.GrainMonth},
	}
	rv, err := ResolveVariables(&def, map[string][]string{})
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), rv.Time.From)
	assert.Equal(t, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), rv.Time.To)
	assert.Equal(t, types.GrainMonth, rv.Time.Grain)
}

func TestResolveVariables_InvalidDefinitionSurfacesValidateError(t *testing.T) {
	def := ViewDefinition{Shape: types.Shape("pie"), Metrics: []types.Metric{types.MetricUsageQuantity}}
	_, err := ResolveVariables(&def, map[string][]string{})
	require.Error(t, err)
}

// TestResolveVariables_RelativeTimeRange proves a literal relative-range
// token ("last_N_days") resolves to a [now-N*24h, now] UTC window.
func TestResolveVariables_RelativeTimeRange(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Range: "last_7_days", Grain: types.GrainDay},
	}
	before := time.Now().UTC()
	rv, err := ResolveVariables(&def, map[string][]string{})
	after := time.Now().UTC()
	require.NoError(t, err)

	assert.WithinDuration(t, before, rv.Time.To, after.Sub(before)+time.Second)
	assert.WithinDuration(t, rv.Time.To.Add(-7*24*time.Hour), rv.Time.From, time.Second)
	assert.Equal(t, types.GrainDay, rv.Time.Grain)
}

// TestResolveVariables_RelativeTimeRangeHours proves the "last_N_hours" form.
func TestResolveVariables_RelativeTimeRangeHours(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Range: "last_24_hours", Grain: types.GrainHour},
	}
	rv, err := ResolveVariables(&def, map[string][]string{})
	require.NoError(t, err)
	assert.WithinDuration(t, rv.Time.To.Add(-24*time.Hour), rv.Time.From, time.Second)
}

// TestResolveVariables_RelativeTimeRangeToday proves the "today" token.
func TestResolveVariables_RelativeTimeRangeToday(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Range: "today", Grain: types.GrainHour},
	}
	rv, err := ResolveVariables(&def, map[string][]string{})
	require.NoError(t, err)
	now := time.Now().UTC()
	assert.Equal(t, time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC), rv.Time.From)
}

// TestResolveVariables_RelativeTimeRangeUnrecognizedToken proves an
// unrecognized relative token is a validation error, not a silent no-op.
func TestResolveVariables_RelativeTimeRangeUnrecognizedToken(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Range: "last_week", Grain: types.GrainDay},
	}
	_, err := ResolveVariables(&def, map[string][]string{})
	require.Error(t, err)
}

// TestResolveVariables_OmittedTimeDefaultsToLast7Days proves time is
// optional: a ViewDefinition with no Time set at all still resolves,
// defaulting to a 7-day window rather than erroring.
func TestResolveVariables_OmittedTimeDefaultsToLast7Days(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		// Time intentionally left zero-valued.
	}
	rv, err := ResolveVariables(&def, map[string][]string{})
	require.NoError(t, err)
	assert.WithinDuration(t, rv.Time.To.Add(-7*24*time.Hour), rv.Time.From, time.Second)
}

// TestResolveVariables_AppliesDefaultWhenAbsent proves an unsupplied variable
// falls back to its declared Default rather than resolving empty.
func TestResolveVariables_AppliesDefaultWhenAbsent(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*Filter{
			{Field: "meter_id", Op: types.EQUAL, Value: []string{"{{meter}}"}},
		},
		Time:      TimeSpecRaw{Range: "2026-08-01..2026-09-01", Grain: types.GrainDay},
		Variables: []*Variable{{Name: "meter", Type: types.VariableTypeString, Default: []string{"meter_default"}}},
	}
	rv, err := ResolveVariables(&def, map[string][]string{})
	require.NoError(t, err)
	require.Len(t, rv.Filters, 1)
	assert.Equal(t, []string{"meter_default"}, rv.Filters[0].Value)
}

// TestResolveVariables_DefaultSatisfiesRequired proves a required variable with
// a Default is not treated as missing when the caller omits it.
func TestResolveVariables_DefaultSatisfiesRequired(t *testing.T) {
	def := ViewDefinition{
		Shape:     types.ShapeTimeseries,
		Metrics:   []types.Metric{types.MetricUsageQuantity},
		Time:      TimeSpecRaw{Range: "{{dr}}", Grain: types.GrainDay},
		Variables: []*Variable{{Name: "dr", Type: types.VariableTypeDateRange, Required: true, Default: []string{"last_30_days"}}},
	}
	rv, err := ResolveVariables(&def, map[string][]string{})
	require.NoError(t, err)
	assert.WithinDuration(t, rv.Time.To.Add(-30*24*time.Hour), rv.Time.From, time.Second)
}

// TestResolveVariables_SuppliedOverridesDefault proves a supplied value wins
// over the declared Default.
func TestResolveVariables_SuppliedOverridesDefault(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*Filter{{Field: "meter_id", Op: types.EQUAL, Value: []string{"{{meter}}"}}},
		Time:    TimeSpecRaw{Range: "2026-08-01..2026-09-01", Grain: types.GrainDay},
		Variables: []*Variable{
			{Name: "meter", Type: types.VariableTypeString, Default: []string{"meter_default"}},
		},
	}
	rv, err := ResolveVariables(&def, map[string][]string{"meter": {"meter_supplied"}})
	require.NoError(t, err)
	require.Len(t, rv.Filters, 1)
	assert.Equal(t, []string{"meter_supplied"}, rv.Filters[0].Value)
}

// TestResolveVariables_NumberTypeRejectsNonNumeric proves a number variable
// validates its value shape.
func TestResolveVariables_NumberTypeRejectsNonNumeric(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*Filter{{Field: "threshold", Op: types.EQUAL, Value: []string{"{{n}}"}}},
		Time:    TimeSpecRaw{Range: "2026-08-01..2026-09-01", Grain: types.GrainDay},
		Variables: []*Variable{
			{Name: "n", Type: types.VariableTypeNumber},
		},
	}
	_, err := ResolveVariables(&def, map[string][]string{"n": {"not_a_number"}})
	require.Error(t, err)
}

// TestResolveVariables_BooleanTypeRejectsNonBoolean proves a boolean variable
// validates its value shape.
func TestResolveVariables_BooleanTypeRejectsNonBoolean(t *testing.T) {
	def := ViewDefinition{
		Shape:     types.ShapeBreakdown,
		Metrics:   []types.Metric{types.MetricUsageQuantity},
		Filters:   []*Filter{{Field: "flag", Op: types.EQUAL, Value: []string{"{{b}}"}}},
		Time:      TimeSpecRaw{Range: "2026-08-01..2026-09-01", Grain: types.GrainDay},
		Variables: []*Variable{{Name: "b", Type: types.VariableTypeBoolean}},
	}
	_, err := ResolveVariables(&def, map[string][]string{"b": {"maybe"}})
	require.Error(t, err)
}

func TestResolveVariables_PreservesShapeMetricsDimensionsSortLimit(t *testing.T) {
	def := ViewDefinition{
		Shape:      types.ShapeBreakdown,
		Metrics:    []types.Metric{types.MetricUsageQuantity, types.MetricEventCount},
		Dimensions: []string{"customer_id"},
		Sort:       []*SortSpec{{Field: "usage_quantity", Dir: types.SortDirectionDesc}},
		Limit:      25,
		Time:       TimeSpecRaw{Range: "2026-08-01..2026-09-01", Grain: types.GrainDay},
	}
	rv, err := ResolveVariables(&def, map[string][]string{})
	require.NoError(t, err)
	assert.Equal(t, types.ShapeBreakdown, rv.Shape)
	assert.Equal(t, []types.Metric{types.MetricUsageQuantity, types.MetricEventCount}, rv.Metrics)
	assert.Equal(t, []string{"customer_id"}, rv.Dimensions)
	assert.Equal(t, []*SortSpec{{Field: "usage_quantity", Dir: types.SortDirectionDesc}}, rv.Sort)
	assert.Equal(t, 25, rv.Limit)
}
