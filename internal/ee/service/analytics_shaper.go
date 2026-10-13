package service

import (
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// dimensionValue reads dim's value off a detailed usage-analytics item. dim
// is the ORIGINAL dimension name the view requested (pre alias-translation —
// see translateDimensions), so "customer_id" and "external_customer_id" both
// resolve to item.ExternalCustomerID.
//
// Money guard: this reads only usage-facing fields off the item
// (MeterID/Source/ExternalCustomerID/Properties) — never Subtotal,
// TotalCost, TotalDiscount, Currency, or CommitmentInfo. Phase-1 is
// usage-only and must never surface money (see analytics_shaper_test.go).
func dimensionValue(item dto.UsageAnalyticItem, dim string) string {
	switch {
	case dim == "meter_id":
		return item.MeterID
	case dim == "source":
		return item.Source
	case dim == "customer_id" || dim == "external_customer_id":
		return item.ExternalCustomerID
	case strings.HasPrefix(dim, "properties."):
		return item.Properties[strings.TrimPrefix(dim, "properties.")]
	default:
		return ""
	}
}

// effectiveMetrics defaults an empty metrics list to usage_quantity
func effectiveMetrics(metrics []types.Metric) []types.Metric {
	if len(metrics) == 0 {
		return []types.Metric{types.MetricUsageQuantity}
	}
	return metrics
}

// metricValue reads m's value off a breakdown item. item.TotalUsage is
// already the meter's resolved aggregation value (SUM/MAX/LATEST/
// COUNT_UNIQUE — see buildMeterUsageAggregationColumns), so no
// per-aggregation routing is needed here.
//
// Money guard: only item.TotalUsage/item.EventCount (usage) are read —
// never item.Subtotal/TotalCost/TotalDiscount/Currency/CommitmentInfo.
func metricValue(item dto.UsageAnalyticItem, m types.Metric) string {
	if m == types.MetricEventCount {
		return strconv.FormatUint(item.EventCount, 10)
	}
	return item.TotalUsage.String()
}

// pointMetricValue mirrors metricValue for a single timeseries bucket point.
//
// Money guard: only p.Usage/p.EventCount are read — never p.Subtotal/
// Discount/Cost or the commitment-bucket cost fields.
func pointMetricValue(p dto.UsageAnalyticPoint, m types.Metric) string {
	if m == types.MetricEventCount {
		return strconv.FormatUint(p.EventCount, 10)
	}
	return p.Usage.String()
}

// shapeBreakdown converts detailed usage-analytics items into a
// dimension+metric table: one row per item, one column per requested
// dimension, one column per requested metric (defaulting to usage_quantity
// when none are requested), plus unit/unit_plural usage-descriptor columns.
//
// Money guard: only usage fields (TotalUsage/EventCount/Unit/UnitPlural) are
// read — never Subtotal/TotalCost/TotalDiscount/Currency/CommitmentInfo.
func shapeBreakdown(items []dto.UsageAnalyticItem, dims []string, metrics []types.Metric) dto.AnalyticsQueryResult {
	metrics = effectiveMetrics(metrics)

	cols := make([]*dto.AnalyticsColumn, 0, len(dims)+len(metrics)+2)
	for _, d := range dims {
		cols = append(cols, dto.NewAnalyticsColumn(d, types.ColumnTypeString, types.ColumnRoleDimension, ""))
	}
	for _, m := range metrics {
		cols = append(cols, dto.NewAnalyticsColumn(string(m), types.ColumnTypeDecimal, types.ColumnRoleMetric, ""))
	}
	cols = append(cols,
		dto.NewAnalyticsColumn("unit", types.ColumnTypeString, types.ColumnRoleDimension, ""),
		dto.NewAnalyticsColumn("unit_plural", types.ColumnTypeString, types.ColumnRoleDimension, ""),
	)

	rows := make([][]string, 0, len(items))
	for _, it := range items {
		row := make([]string, 0, len(cols))
		for _, d := range dims {
			row = append(row, dimensionValue(it, d))
		}
		for _, m := range metrics {
			row = append(row, metricValue(it, m))
		}
		row = append(row, it.Unit, it.UnitPlural)
		rows = append(rows, row)
	}
	return dto.AnalyticsQueryResult{Columns: cols, Rows: rows, Meta: dto.AnalyticsQueryMeta{QuerySource: "meter_usage"}}
}

// shapeTimeseries converts detailed usage-analytics items' time-bucketed
// points into a window_start(+dims)+metric(s)+unit table: one row per
// (item, point) pair. No cross-bucket total is computed in meta — summing
// bucket values is only correct for additive aggregations (e.g. SUM), not
// MAX/LATEST/COUNT_UNIQUE.
//
// Money guard: only p.Usage/p.EventCount/item.Unit/item.UnitPlural are read
// off each point/item — never p.Subtotal/Discount/Cost or the
// commitment-bucket cost fields.
func shapeTimeseries(items []dto.UsageAnalyticItem, dims []string, metrics []types.Metric) dto.AnalyticsQueryResult {
	metrics = effectiveMetrics(metrics)

	cols := make([]*dto.AnalyticsColumn, 0, len(dims)+len(metrics)+3)
	cols = append(cols, dto.NewAnalyticsColumn("window_start", types.ColumnTypeDatetime, types.ColumnRoleDimension, ""))
	for _, d := range dims {
		cols = append(cols, dto.NewAnalyticsColumn(d, types.ColumnTypeString, types.ColumnRoleDimension, ""))
	}
	for _, m := range metrics {
		cols = append(cols, dto.NewAnalyticsColumn(string(m), types.ColumnTypeDecimal, types.ColumnRoleMetric, ""))
	}
	cols = append(cols,
		dto.NewAnalyticsColumn("unit", types.ColumnTypeString, types.ColumnRoleDimension, ""),
		dto.NewAnalyticsColumn("unit_plural", types.ColumnTypeString, types.ColumnRoleDimension, ""),
	)

	rows := make([][]string, 0)
	for _, it := range items {
		for _, p := range it.Points {
			row := make([]string, 0, len(cols))
			row = append(row, p.Timestamp.Format(time.RFC3339))
			for _, d := range dims {
				row = append(row, dimensionValue(it, d))
			}
			for _, m := range metrics {
				row = append(row, pointMetricValue(p, m))
			}
			row = append(row, it.Unit, it.UnitPlural)
			rows = append(rows, row)
		}
	}
	return dto.AnalyticsQueryResult{
		Columns: cols,
		Rows:    rows,
		Meta:    dto.AnalyticsQueryMeta{QuerySource: "meter_usage"},
	}
}
