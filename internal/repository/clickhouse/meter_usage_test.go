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
	result := formatWindowSize(types.WindowSizeDay)
	assert.Equal(s.T(), "toStartOfDay(timestamp)", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_Hour() {
	result := formatWindowSize(types.WindowSizeHour)
	assert.Equal(s.T(), "toStartOfHour(timestamp)", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_Month() {
	result := formatWindowSize(types.WindowSizeMonth)
	assert.Equal(s.T(), "toStartOfMonth(timestamp)", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_Empty() {
	result := formatWindowSize("")
	assert.Equal(s.T(), "", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_WithBillingAnchor() {
	anchor := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	result := formatWindowSizeWithBillingAnchor(types.WindowSizeMonth, &anchor)
	assert.Contains(s.T(), result, "addDays")
	assert.Contains(s.T(), result, "toStartOfMonth")
}

func (s *MeterUsageQuerySuite) TestWindowSize_MonthNoBillingAnchor() {
	result := formatWindowSizeWithBillingAnchor(types.WindowSizeMonth, nil)
	assert.Equal(s.T(), "toStartOfMonth(timestamp)", result)
}

func (s *MeterUsageQuerySuite) TestWindowSize_DayIgnoresBillingAnchor() {
	anchor := time.Date(2024, 3, 15, 0, 0, 0, 0, time.UTC)
	result := formatWindowSizeWithBillingAnchor(types.WindowSizeDay, &anchor)
	assert.Equal(s.T(), "toStartOfDay(timestamp)", result)
}
