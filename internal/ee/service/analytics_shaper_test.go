package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestShapeBreakdown_ColumnsAndRows(t *testing.T) {
	items := []dto.UsageAnalyticItem{
		{Properties: map[string]string{"region": "us"}, TotalUsage: decimal.NewFromInt(60000), Unit: "call", UnitPlural: "calls"},
		{Properties: map[string]string{"region": "eu"}, TotalUsage: decimal.NewFromInt(30000), Unit: "call", UnitPlural: "calls"},
	}
	got := shapeBreakdown(items, []string{"properties.region"}, nil)
	require.Len(t, got.Columns, 4)
	assert.Equal(t, "properties.region", got.Columns[0].Name)
	assert.Equal(t, types.ColumnRoleDimension, got.Columns[0].Role)
	assert.Equal(t, "usage_quantity", got.Columns[1].Name)
	assert.Equal(t, types.ColumnRoleMetric, got.Columns[1].Role)
	assert.Equal(t, "unit", got.Columns[2].Name)
	assert.Equal(t, types.ColumnRoleDimension, got.Columns[2].Role)
	assert.Equal(t, "unit_plural", got.Columns[3].Name)
	assert.Len(t, got.Rows, 2)
	assert.Equal(t, []string{"us", "60000", "call", "calls"}, got.Rows[0])
	assert.Equal(t, []string{"eu", "30000", "call", "calls"}, got.Rows[1])
	assert.Equal(t, "meter_usage", got.Meta.QuerySource)
}

func TestShapeBreakdown_EmptyItems(t *testing.T) {
	got := shapeBreakdown(nil, []string{"properties.region"}, nil)
	assert.Len(t, got.Columns, 4) // dim + usage_quantity + unit + unit_plural
	assert.Len(t, got.Rows, 0)
}

func TestShapeBreakdown_NoDims(t *testing.T) {
	items := []dto.UsageAnalyticItem{
		{TotalUsage: decimal.NewFromInt(100)},
	}
	got := shapeBreakdown(items, nil, nil)
	assert.Len(t, got.Columns, 3) // usage_quantity + unit + unit_plural
	assert.Equal(t, "usage_quantity", got.Columns[0].Name)
	assert.Equal(t, []string{"100", "", ""}, got.Rows[0])
}

// TestShapeBreakdown_StructuralDimsPopulate proves meter_id/source/
// external_customer_id (not just properties.*) populate correctly — the old
// shaper only read item.Properties.
func TestShapeBreakdown_StructuralDimsPopulate(t *testing.T) {
	items := []dto.UsageAnalyticItem{
		{
			MeterID:            "meter_1",
			Source:             "api",
			ExternalCustomerID: "cust_1",
			TotalUsage:         decimal.NewFromInt(42),
		},
	}
	got := shapeBreakdown(items, []string{"meter_id", "source", "customer_id"}, nil)
	assert.Equal(t, []string{"meter_1", "api", "cust_1", "42", "", ""}, got.Rows[0])
}

// TestShapeBreakdown_MetricsMapToColumns proves the view's requested metrics
// map onto one column each, in order, and that an event_count metric reads
// item.EventCount (not TotalUsage).
func TestShapeBreakdown_MetricsMapToColumns(t *testing.T) {
	items := []dto.UsageAnalyticItem{
		{TotalUsage: decimal.NewFromInt(42), EventCount: 7, Unit: "call", UnitPlural: "calls"},
	}
	got := shapeBreakdown(items, nil, []types.Metric{types.MetricUsageQuantity, types.MetricEventCount})
	assert.Equal(t, []string{"usage_quantity", "event_count", "unit", "unit_plural"}, columnNames(got.Columns))
	for _, c := range got.Columns[:2] {
		assert.Equal(t, types.ColumnRoleMetric, c.Role)
	}
	assert.Equal(t, []string{"42", "7", "call", "calls"}, got.Rows[0])
}

// TestShapeBreakdown_EmptyMetricsDefaultsToUsageQuantity proves an empty
// Metrics list still yields the usage_quantity column, matching the
// pre-Fix3 default behavior.
func TestShapeBreakdown_EmptyMetricsDefaultsToUsageQuantity(t *testing.T) {
	items := []dto.UsageAnalyticItem{{TotalUsage: decimal.NewFromInt(9)}}
	got := shapeBreakdown(items, nil, nil)
	assert.Equal(t, "usage_quantity", got.Columns[0].Name)
	assert.Equal(t, "9", got.Rows[0][0])
}

func TestShapeTimeseries_ColumnsAndRows(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	items := []dto.UsageAnalyticItem{
		{
			MeterID: "meter_1",
			Points: []dto.UsageAnalyticPoint{
				{Timestamp: t1, Usage: decimal.NewFromInt(100)},
				{Timestamp: t2, Usage: decimal.NewFromInt(50)},
			},
		},
	}
	got := shapeTimeseries(items, nil, nil)
	require.Len(t, got.Columns, 4) // window_start + usage_quantity + unit + unit_plural
	assert.Equal(t, "window_start", got.Columns[0].Name)
	assert.Equal(t, types.ColumnRoleDimension, got.Columns[0].Role)
	assert.Equal(t, "usage_quantity", got.Columns[1].Name)
	assert.Equal(t, types.ColumnRoleMetric, got.Columns[1].Role)
	assert.Len(t, got.Rows, 2)
	assert.Equal(t, t1.Format(time.RFC3339), got.Rows[0][0])
	assert.Equal(t, "100", got.Rows[0][1])
	assert.Equal(t, "meter_usage", got.Meta.QuerySource)
}

func TestShapeTimeseries_EmptyPoints(t *testing.T) {
	got := shapeTimeseries(nil, nil, nil)
	assert.Len(t, got.Rows, 0)
}

// TestShapeTimeseries_WithDimensionAndWindow proves a split-by dimension is
// carried on every bucketed row alongside window_start, one row per
// (item, point) pair — buckets from different group combos stay distinct.
func TestShapeTimeseries_WithDimensionAndWindow(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []dto.UsageAnalyticItem{
		{
			Properties: map[string]string{"region": "us"},
			Points:     []dto.UsageAnalyticPoint{{Timestamp: t1, Usage: decimal.NewFromInt(10)}},
		},
		{
			Properties: map[string]string{"region": "eu"},
			Points:     []dto.UsageAnalyticPoint{{Timestamp: t1, Usage: decimal.NewFromInt(20)}},
		},
	}
	got := shapeTimeseries(items, []string{"properties.region"}, nil)
	assert.Equal(t, []string{"window_start", "properties.region", "usage_quantity", "unit", "unit_plural"}, columnNames(got.Columns))
	assert.Len(t, got.Rows, 2)
	assert.Equal(t, []string{t1.Format(time.RFC3339), "us", "10", "", ""}, got.Rows[0])
	assert.Equal(t, []string{t1.Format(time.RFC3339), "eu", "20", "", ""}, got.Rows[1])
}

// TestShapeTimeseries_MetricsMapToColumns mirrors the breakdown metric-column
// test: a requested event_count metric reads p.EventCount off each point.
func TestShapeTimeseries_MetricsMapToColumns(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []dto.UsageAnalyticItem{
		{
			Unit:       "call",
			UnitPlural: "calls",
			Points:     []dto.UsageAnalyticPoint{{Timestamp: t1, Usage: decimal.NewFromInt(10), EventCount: 3}},
		},
	}
	got := shapeTimeseries(items, nil, []types.Metric{types.MetricUsageQuantity, types.MetricEventCount})
	assert.Equal(t, []string{"window_start", "usage_quantity", "event_count", "unit", "unit_plural"}, columnNames(got.Columns))
	assert.Equal(t, []string{t1.Format(time.RFC3339), "10", "3", "call", "calls"}, got.Rows[0])
}

func columnNames(cols []*dto.AnalyticsColumn) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}

// TestShapeBreakdown_NeverSurfacesMoney is the Phase-1 money guard: it feeds
// the shaper items carrying money fields with distinctive sentinel values
// and asserts none of them appear anywhere in the marshaled result. Phase-1
// is usage-only — the shaper must read TotalUsage and never
// Subtotal/TotalCost/TotalDiscount/Currency/CommitmentInfo.
func TestShapeBreakdown_NeverSurfacesMoney(t *testing.T) {
	items := []dto.UsageAnalyticItem{
		{
			MeterID:        "meter_1",
			TotalUsage:     decimal.NewFromInt(42),
			Subtotal:       decimal.NewFromInt(111111),
			TotalDiscount:  decimal.NewFromInt(222222),
			TotalCost:      decimal.NewFromInt(333333),
			Currency:       "do_not_leak_currency",
			CommitmentInfo: &types.CommitmentInfo{Amount: decimal.NewFromInt(444444)},
		},
	}
	got := shapeBreakdown(items, []string{"meter_id"}, nil)
	assertNoMoneyLeak(t, got)
}

// TestShapeTimeseries_NeverSurfacesMoney mirrors the breakdown guard for the
// per-point cost fields on UsageAnalyticPoint.
func TestShapeTimeseries_NeverSurfacesMoney(t *testing.T) {
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []dto.UsageAnalyticItem{
		{
			MeterID:    "meter_1",
			TotalUsage: decimal.NewFromInt(42),
			Currency:   "do_not_leak_currency",
			Points: []dto.UsageAnalyticPoint{
				{
					Timestamp:                        t1,
					Usage:                            decimal.NewFromInt(5),
					Subtotal:                         decimal.NewFromInt(555555),
					Discount:                         decimal.NewFromInt(666666),
					Cost:                             decimal.NewFromInt(777777),
					ComputedCommitmentUtilizedAmount: decimal.NewFromInt(888888),
					ComputedOverageAmount:            decimal.NewFromInt(999999),
					ComputedTrueUpAmount:             decimal.NewFromInt(101010),
				},
			},
		},
	}
	got := shapeTimeseries(items, []string{"meter_id"}, nil)
	assertNoMoneyLeak(t, got)
}

func assertNoMoneyLeak(t *testing.T, got dto.AnalyticsQueryResult) {
	t.Helper()
	b, err := json.Marshal(got)
	require.NoError(t, err)
	body := string(b)
	forbidden := []string{
		"111111", "222222", "333333", "444444",
		"555555", "666666", "777777", "888888", "999999", "101010",
		"do_not_leak_currency",
		"subtotal", "total_cost", "total_discount", "currency", "commitment_info", "discount", "cost",
	}
	for _, f := range forbidden {
		assert.NotContains(t, body, f, "money-field content %q leaked into analytics result", f)
	}
}
