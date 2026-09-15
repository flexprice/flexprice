package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

// Shape is the visual/tabular shape an analytics view renders into.
type Shape string

const (
	ShapeTimeseries Shape = "timeseries"
	ShapeBreakdown  Shape = "breakdown"
)

// Grain is the timeseries bucketing granularity. It is the wire-facing enum;
// ToWindowSize maps it onto the engine's internal WindowSize.
type Grain string

const (
	GrainHour  Grain = "hour"
	GrainDay   Grain = "day"
	GrainWeek  Grain = "week"
	GrainMonth Grain = "month"
)

func (g Grain) Validate() error {
	switch g {
	case "", GrainHour, GrainDay, GrainWeek, GrainMonth:
		return nil
	default:
		return ierr.NewErrorf("invalid grain %q", g).
			WithHint("grain must be one of: hour, day, week, month").
			Mark(ierr.ErrValidation)
	}
}

// ToWindowSize maps a validated Grain onto the meter_usage engine's
// WindowSize. An empty (unset) grain maps to an empty window size.
func (g Grain) ToWindowSize() WindowSize {
	switch g {
	case GrainHour:
		return WindowSizeHour
	case GrainDay:
		return WindowSizeDay
	case GrainWeek:
		return WindowSizeWeek
	case GrainMonth:
		return WindowSizeMonth
	default:
		return ""
	}
}

// VariableType is the declared type of a view Variable placeholder.
type VariableType string

const (
	VariableTypeDateRange  VariableType = "date_range"
	VariableTypeString     VariableType = "string"
	VariableTypeStringList VariableType = "string_list"
	VariableTypeNumber     VariableType = "number"
	VariableTypeEnum       VariableType = "enum"
	VariableTypeBoolean    VariableType = "boolean"
)

func (t VariableType) Validate() error {
	switch t {
	case VariableTypeDateRange, VariableTypeString, VariableTypeStringList, VariableTypeNumber, VariableTypeEnum, VariableTypeBoolean:
		return nil
	default:
		return ierr.NewErrorf("invalid variable type %q", t).
			WithHint("type must be one of: date_range, string, string_list, number, enum, boolean").
			Mark(ierr.ErrValidation)
	}
}

// Metric is a Phase-1 usage-only metric a view can request.
type Metric string

const (
	MetricUsageQuantity Metric = "usage_quantity"
	MetricEventCount    Metric = "event_count"
)

func (m Metric) Validate() error {
	switch m {
	case MetricUsageQuantity, MetricEventCount:
		return nil
	default:
		return ierr.NewErrorf("invalid metric %q", m).
			WithHint("metric must be one of: usage_quantity, event_count").
			Mark(ierr.ErrValidation)
	}
}

// ColumnRole classifies an AnalyticsColumn as a dimension or a metric.
type ColumnRole string

const (
	ColumnRoleDimension ColumnRole = "dimension"
	ColumnRoleMetric    ColumnRole = "metric"
)

func (r ColumnRole) Validate() error {
	switch r {
	case ColumnRoleDimension, ColumnRoleMetric:
		return nil
	default:
		return ierr.NewErrorf("invalid column role %q", r).
			WithHint("role must be one of: dimension, metric").
			Mark(ierr.ErrValidation)
	}
}

// ColumnType is the rendering type of an AnalyticsColumn's values.
type ColumnType string

const (
	ColumnTypeString   ColumnType = "string"
	ColumnTypeDecimal  ColumnType = "decimal"
	ColumnTypeDatetime ColumnType = "datetime"
)

func (t ColumnType) Validate() error {
	switch t {
	case ColumnTypeString, ColumnTypeDecimal, ColumnTypeDatetime:
		return nil
	default:
		return ierr.NewErrorf("invalid column type %q", t).
			WithHint("type must be one of: string, decimal, datetime").
			Mark(ierr.ErrValidation)
	}
}
