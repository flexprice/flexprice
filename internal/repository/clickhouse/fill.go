package clickhouse

import (
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// BuildWithFillClause returns the WITH FILL clause for ClickHouse ORDER BY
// to fill missing time windows (e.g. with 0) for commitment calculations.
// Used by both feature_usage and event aggregators.
func BuildWithFillClause(windowSize types.WindowSize, startTime, endTime time.Time) string {
	startTimeStr := startTime.Format("2006-01-02 15:04:05")
	endTimeStr := endTime.Format("2006-01-02 15:04:05")
	stepInterval := getStepIntervalForFill(windowSize)
	boundExpr := getTimeBoundExprForFill(windowSize) // same expression shape for FROM and TO
	fromExpr := fmt.Sprintf(boundExpr, startTimeStr)
	toExpr := fmt.Sprintf(boundExpr, endTimeStr)
	return fmt.Sprintf("WITH FILL FROM %s TO %s STEP %s", fromExpr, toExpr, stepInterval)
}

func getStepIntervalForFill(windowSize types.WindowSize) string {
	switch windowSize {
	case types.WindowSizeMinute:
		return "INTERVAL 1 MINUTE"
	case types.WindowSize15Min:
		return "INTERVAL 15 MINUTE"
	case types.WindowSize30Min:
		return "INTERVAL 30 MINUTE"
	case types.WindowSizeHour:
		return "INTERVAL 1 HOUR"
	case types.WindowSize3Hour:
		return "INTERVAL 3 HOUR"
	case types.WindowSize6Hour:
		return "INTERVAL 6 HOUR"
	case types.WindowSize12Hour:
		return "INTERVAL 12 HOUR"
	case types.WindowSizeDay:
		return "INTERVAL 1 DAY"
	case types.WindowSizeWeek:
		return "INTERVAL 7 DAY"
	case types.WindowSizeMonth:
		return "INTERVAL 1 MONTH"
	default:
		return "INTERVAL 1 HOUR"
	}
}

// getTimeBoundExprForFill returns the format string for a time bound (FROM or TO).
// Use with fmt.Sprintf(expr, timeStr). Same expression is used for both bounds.
func getTimeBoundExprForFill(windowSize types.WindowSize) string {
	switch windowSize {
	case types.WindowSizeMinute:
		return "toStartOfMinute(toDateTime('%s'))"
	case types.WindowSize15Min:
		return "toStartOfInterval(toDateTime('%s'), INTERVAL 15 MINUTE)"
	case types.WindowSize30Min:
		return "toStartOfInterval(toDateTime('%s'), INTERVAL 30 MINUTE)"
	case types.WindowSizeHour:
		return "toStartOfHour(toDateTime('%s'))"
	case types.WindowSize3Hour:
		return "toStartOfInterval(toDateTime('%s'), INTERVAL 3 HOUR)"
	case types.WindowSize6Hour:
		return "toStartOfInterval(toDateTime('%s'), INTERVAL 6 HOUR)"
	case types.WindowSize12Hour:
		return "toStartOfInterval(toDateTime('%s'), INTERVAL 12 HOUR)"
	case types.WindowSizeDay:
		return "toStartOfDay(toDateTime('%s'))"
	case types.WindowSizeWeek:
		return "toStartOfWeek(toDateTime('%s'))"
	case types.WindowSizeMonth:
		return "toStartOfMonth(toDateTime('%s'))"
	default:
		return "toStartOfHour(toDateTime('%s'))"
	}
}
