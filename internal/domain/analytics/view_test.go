package analytics

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_AcceptsTimeseriesAndBreakdown(t *testing.T) {
	for _, shape := range []types.Shape{types.ShapeTimeseries, types.ShapeBreakdown} {
		def := ViewDefinition{Shape: shape, Metrics: []types.Metric{types.MetricUsageQuantity}}
		require.NoError(t, def.Validate())
	}
}

func TestValidate_RejectsEmptyMetrics(t *testing.T) {
	def := ViewDefinition{Shape: types.ShapeTimeseries, Metrics: []types.Metric{}}
	require.Error(t, def.Validate())
}

func TestValidate_RejectsNilMetrics(t *testing.T) {
	def := ViewDefinition{Shape: types.ShapeBreakdown}
	require.Error(t, def.Validate())
}

func TestValidate_RejectsInvalidMetric(t *testing.T) {
	def := ViewDefinition{Shape: types.ShapeBreakdown, Metrics: []types.Metric{types.Metric("total_cost")}}
	require.Error(t, def.Validate())
}

func TestValidate_RejectsInvalidDimension(t *testing.T) {
	def := ViewDefinition{Shape: types.ShapeBreakdown, Metrics: []types.Metric{types.MetricUsageQuantity}, Dimensions: []string{"plan_id"}}
	require.Error(t, def.Validate())
}

func TestValidate_AcceptsAllowlistedDimensions(t *testing.T) {
	def := ViewDefinition{
		Shape:      types.ShapeBreakdown,
		Metrics:    []types.Metric{types.MetricUsageQuantity},
		Dimensions: []string{"meter_id", "source", "external_customer_id", "customer_id", "properties.region"},
	}
	require.NoError(t, def.Validate())
}

func TestValidate_RejectsInvalidGrain(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    TimeSpecRaw{Grain: types.Grain("fortnight")},
	}
	require.Error(t, def.Validate())
}

func TestValidate_RejectsInvalidSortDirection(t *testing.T) {
	def := ViewDefinition{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Sort:    []*SortSpec{{Field: "usage_quantity", Dir: types.SortDirection("sideways")}},
	}
	require.Error(t, def.Validate())
}

func TestValidate_RejectsInvalidVariableType(t *testing.T) {
	def := ViewDefinition{
		Shape:     types.ShapeBreakdown,
		Metrics:   []types.Metric{types.MetricUsageQuantity},
		Variables: []*Variable{{Name: "x", Type: types.VariableType("weird")}},
	}
	require.Error(t, def.Validate())
}

func TestNewViewDefinition_SetsAllFields(t *testing.T) {
	filters := []*Filter{{Field: "meter_id", Op: types.EQUAL, Value: []string{"meter_1"}}}
	sort := []*SortSpec{{Field: "usage_quantity", Dir: types.SortDirectionDesc}}
	variables := []*Variable{{Name: "meter", Type: types.VariableTypeString, Required: true}}
	timeSpec := TimeSpecRaw{Range: "{{date_range}}", Grain: types.GrainDay}

	def := NewViewDefinition(
		"my_view",
		types.ShapeBreakdown,
		[]types.Metric{types.MetricUsageQuantity},
		[]string{"customer_id"},
		filters,
		timeSpec,
		sort,
		10,
		variables,
	)

	assert.Equal(t, "my_view", def.Name)
	assert.Equal(t, types.ShapeBreakdown, def.Shape)
	assert.Equal(t, []types.Metric{types.MetricUsageQuantity}, def.Metrics)
	assert.Equal(t, []string{"customer_id"}, def.Dimensions)
	assert.Equal(t, filters, def.Filters)
	assert.Equal(t, timeSpec, def.Time)
	assert.Equal(t, sort, def.Sort)
	assert.Equal(t, 10, def.Limit)
	assert.Equal(t, variables, def.Variables)
	require.NoError(t, def.Validate())
}

func TestValidateDimensions_RejectsEmptyPropertyField(t *testing.T) {
	require.Error(t, ValidateDimensions([]string{"properties."}))
}
