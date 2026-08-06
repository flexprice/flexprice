package clickhouse

import (
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
)

type MeterUsageQuerySuite struct {
	suite.Suite
	qb *MeterUsageQueryBuilder
}

func TestMeterUsageQuery(t *testing.T) {
	suite.Run(t, new(MeterUsageQuerySuite))
}

func (s *MeterUsageQuerySuite) SetupTest() {
	s.qb = NewMeterUsageQueryBuilder()
}

// --- Aggregator + getMeterUsageAggExprs tests ---

func (s *MeterUsageQuerySuite) TestAggregation_SUM() {
	agg := GetMeterUsageAggregator(types.AggregationSum)
	aggExpr, countExpr := getMeterUsageAggExprs(agg)
	assert.Equal(s.T(), "SUM(qty_total)", aggExpr)
	assert.Equal(s.T(), "COUNT(DISTINCT id)", countExpr)
}

func (s *MeterUsageQuerySuite) TestAggregation_COUNT() {
	agg := GetMeterUsageAggregator(types.AggregationCount)
	aggExpr, _ := getMeterUsageAggExprs(agg)
	assert.Equal(s.T(), "COUNT(DISTINCT id)", aggExpr)
}

func (s *MeterUsageQuerySuite) TestAggregation_COUNT_UNIQUE() {
	agg := GetMeterUsageAggregator(types.AggregationCountUnique)
	aggExpr, _ := getMeterUsageAggExprs(agg)
	assert.Equal(s.T(), "COUNT(DISTINCT unique_hash)", aggExpr)
}

func (s *MeterUsageQuerySuite) TestAggregation_MAX() {
	agg := GetMeterUsageAggregator(types.AggregationMax)
	aggExpr, _ := getMeterUsageAggExprs(agg)
	assert.Equal(s.T(), "MAX(qty_total)", aggExpr)
}

func (s *MeterUsageQuerySuite) TestAggregation_AVG() {
	agg := GetMeterUsageAggregator(types.AggregationAvg)
	aggExpr, _ := getMeterUsageAggExprs(agg)
	assert.Equal(s.T(), "AVG(qty_total)", aggExpr)
}

func (s *MeterUsageQuerySuite) TestAggregation_LATEST() {
	agg := GetMeterUsageAggregator(types.AggregationLatest)
	aggExpr, _ := getMeterUsageAggExprs(agg)
	assert.Equal(s.T(), "argMax(qty_total, timestamp)", aggExpr)
}

func (s *MeterUsageQuerySuite) TestAggregation_SUM_WITH_MULTIPLIER() {
	agg := GetMeterUsageAggregator(types.AggregationSumWithMultiplier)
	aggExpr, _ := getMeterUsageAggExprs(agg)
	assert.Equal(s.T(), "SUM(qty_total)", aggExpr)
}

func (s *MeterUsageQuerySuite) TestAggregation_WEIGHTED_SUM() {
	agg := GetMeterUsageAggregator(types.AggregationWeightedSum)
	aggExpr, _ := getMeterUsageAggExprs(agg)
	assert.Equal(s.T(), "SUM(qty_total)", aggExpr)
}

// --- Aggregator GetType tests ---

func (s *MeterUsageQuerySuite) TestAggregatorType() {
	assert.Equal(s.T(), types.AggregationSum, GetMeterUsageAggregator(types.AggregationSum).GetType())
	assert.Equal(s.T(), types.AggregationCount, GetMeterUsageAggregator(types.AggregationCount).GetType())
	assert.Equal(s.T(), types.AggregationCountUnique, GetMeterUsageAggregator(types.AggregationCountUnique).GetType())
	assert.Equal(s.T(), types.AggregationMax, GetMeterUsageAggregator(types.AggregationMax).GetType())
	assert.Equal(s.T(), types.AggregationAvg, GetMeterUsageAggregator(types.AggregationAvg).GetType())
	assert.Equal(s.T(), types.AggregationLatest, GetMeterUsageAggregator(types.AggregationLatest).GetType())
}

// --- MeterUsageQueryBuilder.BuildWhereClause tests ---

func (s *MeterUsageQuerySuite) TestWhereClause_Basic() {
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	params := &events.MeterUsageQueryParams{
		TenantID:           "t1",
		EnvironmentID:      "env1",
		ExternalCustomerID: "cust1",
		MeterID:            "mtr1",
		StartTime:          start,
		EndTime:            end,
	}

	where, args := s.qb.BuildWhereClause(params)

	assert.Contains(s.T(), where, "tenant_id = ?")
	assert.Contains(s.T(), where, "environment_id = ?")
	assert.Contains(s.T(), where, "external_customer_id = ?")
	assert.Contains(s.T(), where, "meter_id = ?")
	assert.Contains(s.T(), where, "timestamp >= ?")
	assert.Contains(s.T(), where, "timestamp < ?")
	assert.Len(s.T(), args, 6)
	assert.Equal(s.T(), "t1", args[0])
	assert.Equal(s.T(), "env1", args[1])
	assert.Equal(s.T(), "cust1", args[2])
	assert.Equal(s.T(), "mtr1", args[3])
}

func (s *MeterUsageQuerySuite) TestWhereClause_MultiMeter() {
	params := &events.MeterUsageQueryParams{
		TenantID:      "t1",
		EnvironmentID: "env1",
		MeterIDs:      []string{"mtr1", "mtr2", "mtr3"},
	}

	where, args := s.qb.BuildWhereClause(params)

	assert.Contains(s.T(), where, "meter_id IN (?, ?, ?)")
	assert.Len(s.T(), args, 5) // tenant + env + 3 meters
}

func (s *MeterUsageQuerySuite) TestWhereClause_NoCustomer() {
	params := &events.MeterUsageQueryParams{
		TenantID:      "t1",
		EnvironmentID: "env1",
		MeterID:       "mtr1",
	}

	where, _ := s.qb.BuildWhereClause(params)

	assert.NotContains(s.T(), where, "external_customer_id")
}

func (s *MeterUsageQuerySuite) TestWhereClause_MultiCustomer() {
	params := &events.MeterUsageQueryParams{
		TenantID:            "t1",
		EnvironmentID:       "env1",
		ExternalCustomerIDs: []string{"cust1", "cust2", "cust3"},
		MeterID:             "mtr1",
	}

	where, args := s.qb.BuildWhereClause(params)

	assert.Contains(s.T(), where, "external_customer_id IN (?, ?, ?)")
	assert.Len(s.T(), args, 6) // tenant + env + 3 customers + meter
}

func (s *MeterUsageQuerySuite) TestWhereClause_SingleCustomerTakesPrecedence() {
	params := &events.MeterUsageQueryParams{
		TenantID:            "t1",
		EnvironmentID:       "env1",
		ExternalCustomerID:  "cust_single",
		ExternalCustomerIDs: []string{"cust1", "cust2"},
	}

	where, args := s.qb.BuildWhereClause(params)

	// Single customer should take precedence, ExternalCustomerIDs ignored
	assert.Contains(s.T(), where, "external_customer_id = ?")
	assert.NotContains(s.T(), where, "IN")
	assert.Len(s.T(), args, 3) // tenant + env + single customer
}

func (s *MeterUsageQuerySuite) TestWhereClause_NoTimeRange() {
	params := &events.MeterUsageQueryParams{
		TenantID:      "t1",
		EnvironmentID: "env1",
	}

	where, args := s.qb.BuildWhereClause(params)

	assert.NotContains(s.T(), where, "timestamp")
	assert.Len(s.T(), args, 2)
}

func (s *MeterUsageQuerySuite) TestWhereClause_CountUnique() {
	params := &events.MeterUsageQueryParams{
		TenantID:        "t1",
		EnvironmentID:   "env1",
		AggregationType: types.AggregationCountUnique,
	}

	where, _ := s.qb.BuildWhereClause(params)

	assert.Contains(s.T(), where, "unique_hash != ''")
}

// --- BuildFinalClause tests ---

func (s *MeterUsageQuerySuite) TestFinalClause_Enabled() {
	finalClause, settings := s.qb.BuildFinalClause(true)
	assert.Equal(s.T(), "FINAL", finalClause)
	assert.Contains(s.T(), settings, "do_not_merge_across_partitions_select_final")
}

func (s *MeterUsageQuerySuite) TestFinalClause_Disabled() {
	finalClause, settings := s.qb.BuildFinalClause(false)
	assert.Equal(s.T(), "", finalClause)
	assert.Equal(s.T(), "", settings)
}

// --- Window size tests (using shared helpers from aggregators.go) ---

func (s *MeterUsageQuerySuite) TestWindowSize_Day() {
	result := formatWindowSize(types.WindowSizeDay, types.DefaultTimezone)
	assert.Equal(s.T(), "toStartOfDay(timestamp, 'UTC')", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_Hour() {
	result := formatWindowSize(types.WindowSizeHour, types.DefaultTimezone)
	assert.Equal(s.T(), "toStartOfHour(timestamp, 'UTC')", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_Month() {
	result := formatWindowSize(types.WindowSizeMonth, types.DefaultTimezone)
	assert.Equal(s.T(), "toStartOfMonth(timestamp, 'UTC')", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_Empty() {
	result := formatWindowSize(types.WindowSize(""), types.DefaultTimezone)
	assert.Equal(s.T(), "", result)
}

// TestWindowSize_BucketableSizes covers the sub-hourly and multi-hour window
// sizes that per-bucket commitment meters may use now that a meter window of up
// to a day is allowed for time-of-day buckets (see ValidateWindowAlignment).
// These must produce a correct ClickHouse interval grouping.
func (s *MeterUsageQuerySuite) TestWindowSize_BucketableSizes() {
	cases := []struct {
		window types.WindowSize
		expect string
	}{
		{types.WindowSize15Min, "toStartOfInterval(timestamp, INTERVAL 15 MINUTE, 'UTC')"},
		{types.WindowSize30Min, "toStartOfInterval(timestamp, INTERVAL 30 MINUTE, 'UTC')"},
		{types.WindowSize3Hour, "toStartOfInterval(timestamp, INTERVAL 3 HOUR, 'UTC')"},
		{types.WindowSize6Hour, "toStartOfInterval(timestamp, INTERVAL 6 HOUR, 'UTC')"},
		{types.WindowSize12Hour, "toStartOfInterval(timestamp, INTERVAL 12 HOUR, 'UTC')"},
	}
	for _, c := range cases {
		assert.Equal(s.T(), c.expect, formatWindowSize(c.window, types.DefaultTimezone), "window %s", c.window)
	}
}

func (s *MeterUsageQuerySuite) TestWindowSize_WithBillingAnchor() {
	anchor := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	result := formatWindowSizeWithBillingAnchor(types.WindowSizeMonth, &anchor, types.DefaultTimezone)
	assert.Contains(s.T(), result, "addDays")
	assert.Contains(s.T(), result, "toStartOfMonth")
}

func (s *MeterUsageQuerySuite) TestWindowSize_MonthNoBillingAnchor() {
	result := formatWindowSizeWithBillingAnchor(types.WindowSizeMonth, nil, types.DefaultTimezone)
	assert.Equal(s.T(), "toStartOfMonth(timestamp, 'UTC')", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_DayIgnoresBillingAnchor() {
	anchor := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	result := formatWindowSizeWithBillingAnchor(types.WindowSizeDay, &anchor, types.DefaultTimezone)
	assert.Equal(s.T(), "toStartOfDay(timestamp, 'UTC')", result)
}

func TestFormatWindowSizeTimezone(t *testing.T) {
	cases := []struct {
		name     string
		window   types.WindowSize
		tz       string
		expected string
	}{
		{
			// Case 1: Empty timezone + WindowSizeDay → UTC, no tz argument
			name:     "empty tz with WindowSizeDay",
			window:   types.WindowSizeDay,
			tz:       "",
			expected: "toStartOfDay(timestamp, 'UTC')",
		},
		{
			// Case 2: Explicit "UTC" + WindowSizeDay → same as empty
			name:     "UTC tz with WindowSizeDay",
			window:   types.WindowSizeDay,
			tz:       types.DefaultTimezone,
			expected: "toStartOfDay(timestamp, 'UTC')",
		},
		{
			// Case 3: IST non-UTC tz + WindowSizeDay → include tz argument
			name:     "Asia/Kolkata tz with WindowSizeDay",
			window:   types.WindowSizeDay,
			tz:       "Asia/Kolkata",
			expected: "toStartOfDay(timestamp, 'Asia/Kolkata')",
		},
		{
			// Case 4: America/New_York tz + WindowSizeMonth → include tz argument
			name:     "America/New_York tz with WindowSizeMonth",
			window:   types.WindowSizeMonth,
			tz:       "America/New_York",
			expected: "toStartOfMonth(timestamp, 'America/New_York')",
		},
		{
			// Case 5: Invalid timezone + WindowSizeDay → falls back to UTC (no tz arg)
			name:     "invalid tz falls back to UTC with WindowSizeDay",
			window:   types.WindowSizeDay,
			tz:       "Not/ATimezone",
			expected: "toStartOfDay(timestamp, 'UTC')",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := formatWindowSize(c.window, c.tz)
			assert.Equal(t, c.expected, got)
		})
	}
}
