package service

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/analytics"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// ---------------------------------------------------------------------------
// translateBreakdown / translateTimeseries (formerly analytics_translator_test.go)
// ---------------------------------------------------------------------------

func TestTranslateBreakdown_InjectsRLSAndGroupBy(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "tenant_1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_1")

	rv := analytics.ResolvedView{
		Shape:      types.ShapeBreakdown,
		Metrics:    []types.Metric{types.MetricUsageQuantity},
		Dimensions: []string{"properties.region"},
		Filters:    []*analytics.Filter{{Field: "meter_id", Op: types.EQUAL, Value: []string{"meter_1"}}},
		Time:       analytics.TimeSpec{From: time.Now().Add(-24 * time.Hour), To: time.Now(), Grain: types.GrainDay},
	}
	p, err := translateBreakdown(ctx, &rv)
	require.NoError(t, err)
	assert.Equal(t, "tenant_1", p.TenantID)
	assert.Equal(t, "env_1", p.EnvironmentID)
	assert.Contains(t, p.MeterIDs, "meter_1")
	assert.Contains(t, p.GroupBy, "properties.region")
}

// Dimension validation is enforced at the view-definition boundary
// (ViewDefinition.Validate, reached via ExecuteView -> ResolveVariables), not
// in the translate* param-mappers, so these assert on Validate directly.
func TestViewDefinition_RejectsIllegalDimension(t *testing.T) {
	def := analytics.ViewDefinition{
		Shape: types.ShapeBreakdown, Metrics: []types.Metric{types.MetricUsageQuantity},
		Dimensions: []string{"properties.region; DROP TABLE"},
	}
	err := def.Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestViewDefinition_RejectsNonAllowlistedDimension(t *testing.T) {
	def := analytics.ViewDefinition{
		Shape:      types.ShapeBreakdown,
		Metrics:    []types.Metric{types.MetricUsageQuantity},
		Dimensions: []string{"plan_id"},
	}
	err := def.Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestTranslateTimeseries_InjectsRLSAndFilters(t *testing.T) {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, "tenant_1")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_1")

	rv := analytics.ResolvedView{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*analytics.Filter{
			{Field: "meter_id", Op: types.EQUAL, Value: []string{"meter_1"}},
			{Field: "customer_id", Op: types.EQUAL, Value: []string{"cust_1"}},
			{Field: "source", Op: types.EQUAL, Value: []string{"api"}},
		},
		Time: analytics.TimeSpec{From: time.Now().Add(-24 * time.Hour), To: time.Now(), Grain: types.GrainHour},
	}
	p, err := translateTimeseries(ctx, &rv)
	require.NoError(t, err)
	assert.Equal(t, "tenant_1", p.TenantID)
	assert.Equal(t, "env_1", p.EnvironmentID)
	assert.Contains(t, p.MeterIDs, "meter_1")
	assert.Contains(t, p.ExternalCustomerIDs, "cust_1")
	assert.Contains(t, p.Sources, "api")
	assert.Equal(t, types.WindowSizeHour, p.WindowSize)
	assert.True(t, p.UseFinal)
}

func TestViewDefinition_TimeseriesRejectsIllegalDimension(t *testing.T) {
	def := analytics.ViewDefinition{
		Shape:      types.ShapeTimeseries,
		Metrics:    []types.Metric{types.MetricUsageQuantity},
		Dimensions: []string{"properties.region; DROP TABLE"},
	}
	err := def.Validate()
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestTranslateBreakdown_PropertyFilterFallsThrough(t *testing.T) {
	ctx := context.Background()
	rv := analytics.ResolvedView{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*analytics.Filter{{Field: "model", Op: types.EQUAL, Value: []string{"gpt-4"}}},
	}
	p, err := translateBreakdown(ctx, &rv)
	require.NoError(t, err)
	assert.Contains(t, p.PropertyFilters["model"], "gpt-4")
}

// TestTranslateBreakdown_PropertyFilterStripsPropertiesPrefix is the Fix 1
// regression test: a filter whose Field is "properties.team" must be keyed
// by the bare property name "team" in PropertyFilters, because the engine's
// BuildDetailedWhereClause consumes PropertyFilters as
// JSONExtractString(properties, <key>) with the bare name — a "properties."
// prefixed key would never match.
func TestTranslateBreakdown_PropertyFilterStripsPropertiesPrefix(t *testing.T) {
	ctx := context.Background()
	rv := analytics.ResolvedView{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*analytics.Filter{{Field: "properties.team", Op: types.EQUAL, Value: []string{"eng"}}},
	}
	p, err := translateBreakdown(ctx, &rv)
	require.NoError(t, err)
	assert.Contains(t, p.PropertyFilters["team"], "eng")
	assert.NotContains(t, p.PropertyFilters, "properties.team")
}

// TestTranslateBreakdown_BarePropertyFieldStillWorks proves a bare property
// name (no "properties." prefix) is accepted the same way, consistent with
// how dimensions are named.
func TestTranslateBreakdown_BarePropertyFieldStillWorks(t *testing.T) {
	ctx := context.Background()
	rv := analytics.ResolvedView{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*analytics.Filter{{Field: "team", Op: types.EQUAL, Value: []string{"eng"}}},
	}
	p, err := translateBreakdown(ctx, &rv)
	require.NoError(t, err)
	assert.Contains(t, p.PropertyFilters["team"], "eng")
}

// ---------------------------------------------------------------------------
// Filter.Op validation (Fix 4)
// ---------------------------------------------------------------------------

// TestTranslateBreakdown_AcceptsEqAndInOps proves the two supported filter
// operators (eq, in) both pass through translation unchanged.
func TestTranslateBreakdown_AcceptsEqAndInOps(t *testing.T) {
	for _, op := range []types.FilterOperatorType{types.EQUAL, types.IN} {
		ctx := context.Background()
		rv := analytics.ResolvedView{
			Shape:   types.ShapeBreakdown,
			Metrics: []types.Metric{types.MetricUsageQuantity},
			Filters: []*analytics.Filter{{Field: "meter_id", Op: op, Value: []string{"meter_1"}}},
		}
		p, err := translateBreakdown(ctx, &rv)
		require.NoError(t, err, "op %q should be accepted", op)
		assert.Contains(t, p.MeterIDs, "meter_1")
	}
}

// TestTranslateBreakdown_RejectsUnsupportedFilterOp proves any operator other
// than eq/in fails loud with a validation error, rather than silently being
// treated as an IN-style filter.
func TestTranslateBreakdown_RejectsUnsupportedFilterOp(t *testing.T) {
	for _, op := range []types.FilterOperatorType{types.GREATER_THAN, types.LESS_THAN, types.CONTAINS, types.NOT_IN} {
		ctx := context.Background()
		rv := analytics.ResolvedView{
			Shape:   types.ShapeBreakdown,
			Metrics: []types.Metric{types.MetricUsageQuantity},
			Filters: []*analytics.Filter{{Field: "meter_id", Op: op, Value: []string{"meter_1"}}},
		}
		_, err := translateBreakdown(ctx, &rv)
		require.Error(t, err, "op %q should be rejected", op)
		assert.True(t, ierr.IsValidation(err))
	}
}

// ---------------------------------------------------------------------------
// sortBreakdownRows / truncateRows (Fix 2)
// ---------------------------------------------------------------------------

func TestSortBreakdownRows_NumericDescByMetric(t *testing.T) {
	res := dto.AnalyticsQueryResult{
		Columns: []*dto.AnalyticsColumn{
			{Name: "meter_id", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
			{Name: "usage_quantity", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
		},
		Rows: [][]string{
			{"m1", "5"},
			{"m2", "20"},
			{"m3", "10"},
		},
	}
	err := sortBreakdownRows(&res, []*analytics.SortSpec{{Field: "usage_quantity", Dir: types.SortDirectionDesc}})
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"m2", "20"}, {"m3", "10"}, {"m1", "5"}}, res.Rows)
}

func TestSortBreakdownRows_EmptyDirDefaultsToAsc(t *testing.T) {
	res := dto.AnalyticsQueryResult{
		Columns: []*dto.AnalyticsColumn{
			{Name: "usage_quantity", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric},
		},
		Rows: [][]string{{"20"}, {"5"}, {"10"}},
	}
	err := sortBreakdownRows(&res, []*analytics.SortSpec{{Field: "usage_quantity"}})
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"5"}, {"10"}, {"20"}}, res.Rows)
}

func TestSortBreakdownRows_LexicalByDimension(t *testing.T) {
	res := dto.AnalyticsQueryResult{
		Columns: []*dto.AnalyticsColumn{
			{Name: "region", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension},
		},
		Rows: [][]string{{"us"}, {"eu"}, {"apac"}},
	}
	err := sortBreakdownRows(&res, []*analytics.SortSpec{{Field: "region", Dir: types.SortDirectionAsc}})
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"apac"}, {"eu"}, {"us"}}, res.Rows)
}

// TestSortBreakdownRows_UnknownFieldFailsLoud proves a SortSpec.Field that
// doesn't match any output column is rejected rather than silently ignored.
func TestSortBreakdownRows_UnknownFieldFailsLoud(t *testing.T) {
	res := dto.AnalyticsQueryResult{
		Columns: []*dto.AnalyticsColumn{{Name: "usage_quantity", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric}},
		Rows:    [][]string{{"5"}},
	}
	err := sortBreakdownRows(&res, []*analytics.SortSpec{{Field: "not_a_column"}})
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestTruncateRows_AppliesLimit(t *testing.T) {
	res := dto.AnalyticsQueryResult{Rows: [][]string{{"1"}, {"2"}, {"3"}}}
	truncateRows(&res, 2)
	assert.Equal(t, [][]string{{"1"}, {"2"}}, res.Rows)
}

func TestTruncateRows_ZeroOrNegativeIsNoOp(t *testing.T) {
	res := dto.AnalyticsQueryResult{Rows: [][]string{{"1"}, {"2"}, {"3"}}}
	truncateRows(&res, 0)
	assert.Len(t, res.Rows, 3)
	truncateRows(&res, -1)
	assert.Len(t, res.Rows, 3)
}

func TestTruncateRows_LimitAboveRowCountIsNoOp(t *testing.T) {
	res := dto.AnalyticsQueryResult{Rows: [][]string{{"1"}, {"2"}}}
	truncateRows(&res, 10)
	assert.Len(t, res.Rows, 2)
}

// TestTranslateBreakdown_LeavesWindowSizeUnset proves breakdown never sets
// WindowSize even when the view carries a grain — breakdown is a single
// aggregate row per group, not a time series (windowing is timeseries-only).
func TestTranslateBreakdown_LeavesWindowSizeUnset(t *testing.T) {
	ctx := context.Background()
	rv := analytics.ResolvedView{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Time:    analytics.TimeSpec{From: time.Now().Add(-24 * time.Hour), To: time.Now(), Grain: types.GrainDay},
	}
	p, err := translateBreakdown(ctx, &rv)
	require.NoError(t, err)
	assert.Equal(t, types.WindowSize(""), p.WindowSize)
}

// TestTranslateBreakdown_EmptyDimensionsAllowed proves breakdown dimensions
// are optional — an empty GroupBy is not rejected; the engine defaults to
// one row per meter.
func TestTranslateBreakdown_EmptyDimensionsAllowed(t *testing.T) {
	ctx := context.Background()
	rv := analytics.ResolvedView{
		Shape:   types.ShapeBreakdown,
		Metrics: []types.Metric{types.MetricUsageQuantity},
	}
	p, err := translateBreakdown(ctx, &rv)
	require.NoError(t, err)
	assert.Empty(t, p.GroupBy)
}

// TestTranslateBreakdown_CustomerIDDimensionAliasesToExternalCustomerID proves
// a "customer_id" dimension is rewritten onto the real "external_customer_id"
// group_by token the meter_usage engine understands.
func TestTranslateBreakdown_CustomerIDDimensionAliasesToExternalCustomerID(t *testing.T) {
	ctx := context.Background()
	rv := analytics.ResolvedView{
		Shape:      types.ShapeBreakdown,
		Metrics:    []types.Metric{types.MetricUsageQuantity},
		Dimensions: []string{"customer_id"},
	}
	p, err := translateBreakdown(ctx, &rv)
	require.NoError(t, err)
	assert.Equal(t, []string{"external_customer_id"}, p.GroupBy)
}

// TestTranslateTimeseries_CustomerIDFilterAliasesToExternalCustomerID proves a
// "customer_id" filter (not just "external_customer_id") maps onto
// ExternalCustomerIDs, matching the dimension alias.
func TestTranslateTimeseries_CustomerIDFilterAliasesToExternalCustomerID(t *testing.T) {
	ctx := context.Background()
	rv := analytics.ResolvedView{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*analytics.Filter{{Field: "customer_id", Op: types.EQUAL, Value: []string{"cust_1"}}},
	}
	p, err := translateTimeseries(ctx, &rv)
	require.NoError(t, err)
	assert.Contains(t, p.ExternalCustomerIDs, "cust_1")
}

// ---------------------------------------------------------------------------
// AnalyticsService — ExecuteView / CreateView / QueryView
// ---------------------------------------------------------------------------

type AnalyticsServiceSuite struct {
	testutil.BaseServiceTestSuite
	svc            AnalyticsService
	meterUsageRepo *testutil.InMemoryMeterUsageStore

	now        time.Time
	rangeStart time.Time
	rangeEnd   time.Time
	sumMeter   *meter.Meter
	maxMeter   *meter.Meter
}

func TestAnalyticsService(t *testing.T) { suite.Run(t, new(AnalyticsServiceSuite)) }

func (s *AnalyticsServiceSuite) SetupTest() {
	s.BaseServiceTestSuite.SetupTest()

	s.meterUsageRepo = s.GetStores().MeterUsageRepo.(*testutil.InMemoryMeterUsageStore)

	s.now = time.Now().UTC()
	s.rangeStart = s.now.Add(-48 * time.Hour)
	s.rangeEnd = s.now.Add(24 * time.Hour)

	ctx := s.GetContext()

	s.sumMeter = &meter.Meter{
		ID:        "meter_sum",
		Name:      "SUM meter",
		EventName: "api_call",
		Aggregation: meter.Aggregation{
			Type: types.AggregationSum,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.sumMeter))

	s.maxMeter = &meter.Meter{
		ID:        "meter_max",
		Name:      "MAX meter",
		EventName: "gauge",
		Aggregation: meter.Aggregation{
			Type: types.AggregationMax,
		},
		BaseModel: types.GetDefaultBaseModel(ctx),
	}
	s.NoError(s.GetStores().MeterRepo.CreateMeter(ctx, s.maxMeter))

	s.svc = NewAnalyticsService(ServiceParams{
		Logger:            s.GetLogger(),
		Config:            s.GetConfig(),
		DB:                s.GetDB(),
		MeterUsageRepo:    s.meterUsageRepo,
		MeterRepo:         s.GetStores().MeterRepo,
		CustomerRepo:      s.GetStores().CustomerRepo,
		FeatureRepo:       s.GetStores().FeatureRepo,
		SubRepo:           s.GetStores().SubscriptionRepo,
		PriceRepo:         s.GetStores().PriceRepo,
		AddonRepo:         s.GetStores().AddonRepo,
		GroupRepo:         s.GetStores().GroupRepo,
		AnalyticsViewRepo: s.GetStores().AnalyticsViewRepo,
	})
}

func (s *AnalyticsServiceSuite) TearDownTest() {
	s.BaseServiceTestSuite.TearDownTest()
	s.meterUsageRepo.Clear()
}

// insertUsage inserts one meter_usage row with the given properties.
func (s *AnalyticsServiceSuite) insertUsage(ctx context.Context, meterID string, ts time.Time, qty float64, props map[string]interface{}) {
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:            types.GenerateUUID(),
				TenantID:      types.GetTenantID(ctx),
				EnvironmentID: types.GetEnvironmentID(ctx),
				Timestamp:     ts,
				EventName:     "api_call",
				Properties:    props,
			},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromFloat(qty),
		},
	}))
}

// insertUsageForCustomer inserts one meter_usage row stamped with an
// external_customer_id, for external_customer_id group-by tests.
func (s *AnalyticsServiceSuite) insertUsageForCustomer(ctx context.Context, meterID, customerID string, ts time.Time, qty float64) {
	s.NoError(s.meterUsageRepo.BulkInsertMeterUsage(ctx, []*events.MeterUsage{
		{
			Event: events.Event{
				ID:                 types.GenerateUUID(),
				TenantID:           types.GetTenantID(ctx),
				EnvironmentID:      types.GetEnvironmentID(ctx),
				ExternalCustomerID: customerID,
				Timestamp:          ts,
				EventName:          "api_call",
			},
			MeterID:  meterID,
			QtyTotal: decimal.NewFromFloat(qty),
		},
	}))
}

// drVars returns the "dr" date_range variable as a single-element
// "<from>..<to>" range string.
func (s *AnalyticsServiceSuite) drVars() map[string][]string {
	return map[string][]string{
		"dr": {s.rangeStart.Format("2006-01-02") + ".." + s.rangeEnd.Format("2006-01-02")},
	}
}

func (s *AnalyticsServiceSuite) breakdownDef() *analytics.ViewDefinition {
	return &analytics.ViewDefinition{
		Shape:      types.ShapeBreakdown,
		Metrics:    []types.Metric{types.MetricUsageQuantity},
		Dimensions: []string{"properties.region"},
		Filters:    []*analytics.Filter{{Field: "meter_id", Op: types.EQUAL, Value: []string{"{{meter}}"}}},
		Time:       analytics.TimeSpecRaw{Range: "{{dr}}", Grain: types.GrainDay},
		Variables: []*analytics.Variable{
			{Name: "meter", Type: types.VariableTypeString, Required: true},
			{Name: "dr", Type: types.VariableTypeDateRange, Required: true},
		},
	}
}

// ---------------------------------------------------------------------------
// ExecuteView — breakdown
// ---------------------------------------------------------------------------

func (s *AnalyticsServiceSuite) TestExecuteView_BreakdownReturnsRows() {
	ctx := s.GetContext()
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 3, map[string]interface{}{"region": "us"})
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 4, map[string]interface{}{"region": "us"})
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 5, map[string]interface{}{"region": "eu"})

	vars := s.drVars()
	vars["meter"] = []string{s.sumMeter.ID}

	res, err := s.svc.ExecuteView(ctx, s.breakdownDef(), vars)
	s.NoError(err)
	s.Require().NotNil(res)
	// meter_id is always injected as an identity dimension, so columns are
	// properties.region, meter_id, usage_quantity, unit, unit_plural.
	s.Require().Len(res.Columns, 5)
	s.Equal("properties.region", res.Columns[0].Name)
	s.Equal(types.ColumnRoleDimension, res.Columns[0].Role)
	s.Equal("meter_id", res.Columns[1].Name)
	s.Equal(types.ColumnRoleDimension, res.Columns[1].Role)
	s.Equal("usage_quantity", res.Columns[2].Name)
	s.Equal(types.ColumnRoleMetric, res.Columns[2].Role)

	s.Require().Len(res.Rows, 2)
	totals := map[string]string{}
	for _, row := range res.Rows {
		s.Require().Len(row, 5)
		totals[row[0]] = row[2]
	}
	s.Equal("7", totals["us"])
	s.Equal("5", totals["eu"])
}

// TestExecuteView_BreakdownByMeterIDSucceeds proves the Ruling C guard is
// gone: a structural dimension like meter_id (not just properties.*) is now
// a legal breakdown dimension, since both shapes route through the same
// detailed meter_usage engine and the shaper reads structural dims directly
// off the response item (see dimensionValue).
func (s *AnalyticsServiceSuite) TestExecuteView_BreakdownByMeterIDSucceeds() {
	ctx := s.GetContext()
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 3, nil)
	s.insertUsage(ctx, s.maxMeter.ID, s.now, 9, nil)

	def := s.breakdownDef()
	def.Dimensions = []string{"meter_id"}
	def.Filters = nil // both meters, no meter_id filter

	vars := s.drVars()
	vars["meter"] = []string{"unused"} // breakdownDef declares "meter" required even though Filters is nil here

	res, err := s.svc.ExecuteView(ctx, def, vars)
	s.NoError(err)
	s.Require().NotNil(res)
	s.Equal("meter_id", res.Columns[0].Name)

	totals := map[string]string{}
	for _, row := range res.Rows {
		totals[row[0]] = row[1]
	}
	s.Equal("3", totals[s.sumMeter.ID])
	s.Equal("9", totals[s.maxMeter.ID])
}

// TestExecuteView_BreakdownZeroDimensionsIsMeterLevel proves dimensions are
// optional for breakdown: an empty Dimensions list yields one row per meter,
// labeled by the always-injected meter_id identity column, not a validation error.
func (s *AnalyticsServiceSuite) TestExecuteView_BreakdownZeroDimensionsIsMeterLevel() {
	ctx := s.GetContext()
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 3, map[string]interface{}{"region": "us"})
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 4, map[string]interface{}{"region": "eu"})

	def := s.breakdownDef()
	def.Dimensions = nil

	vars := s.drVars()
	vars["meter"] = []string{s.sumMeter.ID}

	res, err := s.svc.ExecuteView(ctx, def, vars)
	s.NoError(err)
	s.Require().NotNil(res)
	// meter_id (injected), usage_quantity, unit, unit_plural.
	s.Require().Len(res.Columns, 4)
	s.Equal("meter_id", res.Columns[0].Name)
	s.Equal("usage_quantity", res.Columns[1].Name)
	s.Require().Len(res.Rows, 1) // one row for the meter (both events roll up)
	s.Equal(s.sumMeter.ID, res.Rows[0][0])
	s.Equal("7", res.Rows[0][1])
}

// TestExecuteView_BreakdownMultiMeterInjectsMeterIdentity proves a breakdown
// spanning multiple meters (no single-meter filter, meter_id not already a
// dimension) gains a meter_id column so its rows stay distinguishable.
func (s *AnalyticsServiceSuite) TestExecuteView_BreakdownMultiMeterInjectsMeterIdentity() {
	ctx := s.GetContext()
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 3, nil)
	s.insertUsage(ctx, s.maxMeter.ID, s.now, 9, nil)

	def := s.breakdownDef()
	def.Dimensions = nil
	def.Filters = nil // both meters

	vars := s.drVars()
	vars["meter"] = []string{"unused"} // breakdownDef declares "meter" required

	res, err := s.svc.ExecuteView(ctx, def, vars)
	s.NoError(err)
	s.Require().NotNil(res)
	s.Equal("meter_id", res.Columns[0].Name, "multi-meter breakdown must label rows by meter_id")

	totals := map[string]string{}
	for _, row := range res.Rows {
		totals[row[0]] = row[1]
	}
	s.Equal("3", totals[s.sumMeter.ID])
	s.Equal("9", totals[s.maxMeter.ID])
}

// TestExecuteView_BreakdownGroupsByExternalCustomerID proves the new
// "customer_id" dimension alias (-> "external_customer_id" group_by) lets a
// breakdown view build a per-customer chart — the admin (no-customer-context)
// query path grouping raw meter_usage across all customers.
func (s *AnalyticsServiceSuite) TestExecuteView_BreakdownGroupsByExternalCustomerID() {
	ctx := s.GetContext()
	s.insertUsageForCustomer(ctx, s.sumMeter.ID, "cust_a", s.now, 3)
	s.insertUsageForCustomer(ctx, s.sumMeter.ID, "cust_b", s.now, 5)

	def := s.breakdownDef()
	def.Dimensions = []string{"customer_id"}

	vars := s.drVars()
	vars["meter"] = []string{s.sumMeter.ID}

	res, err := s.svc.ExecuteView(ctx, def, vars)
	s.NoError(err)
	s.Require().NotNil(res)
	// customer_id, then the injected meter_id, then usage_quantity.
	s.Equal("customer_id", res.Columns[0].Name)
	s.Equal("meter_id", res.Columns[1].Name)
	s.Equal("usage_quantity", res.Columns[2].Name)

	totals := map[string]string{}
	for _, row := range res.Rows {
		totals[row[0]] = row[2]
	}
	s.Equal("3", totals["cust_a"])
	s.Equal("5", totals["cust_b"])
}

// TestExecuteView_BreakdownTopNByUsageDesc proves Fix 2 end-to-end: a
// breakdown view's Sort spec ranks the shaped rows by the usage_quantity
// column, descending, through the full ExecuteView pipeline.
func (s *AnalyticsServiceSuite) TestExecuteView_BreakdownTopNByUsageDesc() {
	ctx := s.GetContext()
	s.insertUsageForCustomer(ctx, s.sumMeter.ID, "cust_a", s.now, 3)
	s.insertUsageForCustomer(ctx, s.sumMeter.ID, "cust_b", s.now, 20)
	s.insertUsageForCustomer(ctx, s.sumMeter.ID, "cust_c", s.now, 10)

	def := s.breakdownDef()
	def.Dimensions = []string{"customer_id"}
	def.Sort = []*analytics.SortSpec{{Field: "usage_quantity", Dir: types.SortDirectionDesc}}

	vars := s.drVars()
	vars["meter"] = []string{s.sumMeter.ID}

	res, err := s.svc.ExecuteView(ctx, def, vars)
	s.NoError(err)
	s.Require().NotNil(res)
	s.Require().Len(res.Rows, 3)
	s.Equal([]string{"cust_b", "cust_c", "cust_a"}, []string{res.Rows[0][0], res.Rows[1][0], res.Rows[2][0]})
}

// TestExecuteView_BreakdownLimitTruncates proves Fix 2's Limit is applied
// after sorting, keeping only the top N rows.
func (s *AnalyticsServiceSuite) TestExecuteView_BreakdownLimitTruncates() {
	ctx := s.GetContext()
	s.insertUsageForCustomer(ctx, s.sumMeter.ID, "cust_a", s.now, 3)
	s.insertUsageForCustomer(ctx, s.sumMeter.ID, "cust_b", s.now, 20)
	s.insertUsageForCustomer(ctx, s.sumMeter.ID, "cust_c", s.now, 10)

	def := s.breakdownDef()
	def.Dimensions = []string{"customer_id"}
	def.Sort = []*analytics.SortSpec{{Field: "usage_quantity", Dir: types.SortDirectionDesc}}
	def.Limit = 2

	vars := s.drVars()
	vars["meter"] = []string{s.sumMeter.ID}

	res, err := s.svc.ExecuteView(ctx, def, vars)
	s.NoError(err)
	s.Require().NotNil(res)
	s.Require().Len(res.Rows, 2)
	s.Equal("cust_b", res.Rows[0][0])
	s.Equal("cust_c", res.Rows[1][0])
}

// ---------------------------------------------------------------------------
// ExecuteView — timeseries
// ---------------------------------------------------------------------------

func (s *AnalyticsServiceSuite) timeseriesDef(meterID string) *analytics.ViewDefinition {
	return &analytics.ViewDefinition{
		Shape:   types.ShapeTimeseries,
		Metrics: []types.Metric{types.MetricUsageQuantity},
		Filters: []*analytics.Filter{{Field: "meter_id", Op: types.EQUAL, Value: []string{meterID}}},
		Time:    analytics.TimeSpecRaw{Range: "{{dr}}", Grain: types.GrainDay},
		Variables: []*analytics.Variable{
			{Name: "dr", Type: types.VariableTypeDateRange, Required: true},
		},
	}
}

// TestExecuteView_TimeseriesUsesMeterAggregation proves Ruling A: a MAX meter's
// timeseries execution must apply MAX (not silently default to SUM).
func (s *AnalyticsServiceSuite) TestExecuteView_TimeseriesUsesMeterAggregation() {
	ctx := s.GetContext()
	s.insertUsage(ctx, s.maxMeter.ID, s.now, 5, nil)
	s.insertUsage(ctx, s.maxMeter.ID, s.now, 20, nil)
	s.insertUsage(ctx, s.maxMeter.ID, s.now, 10, nil)

	res, err := s.svc.ExecuteView(ctx, s.timeseriesDef(s.maxMeter.ID), s.drVars())
	s.NoError(err)
	s.Require().NotNil(res)
	// window_start, meter_id (injected), usage_quantity, unit, unit_plural.
	s.Require().Len(res.Columns, 5)
	s.Equal("window_start", res.Columns[0].Name)
	s.Equal("meter_id", res.Columns[1].Name)
	s.Equal("usage_quantity", res.Columns[2].Name)

	// MAX(5, 20, 10) == 20. If AggregationType had silently defaulted to SUM
	// (the bug Ruling A guards against), this would be 35 instead.
	s.Require().Len(res.Rows, 1)
	s.Equal("20", res.Rows[0][2])
}

// TestExecuteView_TimeseriesWithDimensionBucketsByGroup proves timeseries
// dimensions now work as a split-by: one bucketed series per group combo,
// via the same detailed meter_usage engine as breakdown (the earlier cut
// rejected any Dimensions on a timeseries view).
func (s *AnalyticsServiceSuite) TestExecuteView_TimeseriesWithDimensionBucketsByGroup() {
	ctx := s.GetContext()
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 3, map[string]interface{}{"region": "us"})
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 5, map[string]interface{}{"region": "eu"})

	def := s.timeseriesDef(s.sumMeter.ID)
	def.Dimensions = []string{"properties.region"}

	res, err := s.svc.ExecuteView(ctx, def, s.drVars())
	s.NoError(err)
	s.Require().NotNil(res)
	// window_start, properties.region, meter_id (injected), usage_quantity, unit, unit_plural.
	s.Require().Len(res.Columns, 6)
	s.Equal("window_start", res.Columns[0].Name)
	s.Equal("properties.region", res.Columns[1].Name)
	s.Equal("meter_id", res.Columns[2].Name)
	s.Equal("usage_quantity", res.Columns[3].Name)

	totals := map[string]string{}
	for _, row := range res.Rows {
		s.Require().Len(row, 6)
		totals[row[1]] = row[3]
	}
	s.Equal("3", totals["us"])
	s.Equal("5", totals["eu"])
}

// TestExecuteView_TimeseriesMultipleMetersAllowed proves timeseries no
// longer requires exactly one meter: the admin-path engine derives each
// meter's own aggregation type independently (Ruling A still holds per
// meter — see TestExecuteView_TimeseriesUsesMeterAggregation).
func (s *AnalyticsServiceSuite) TestExecuteView_TimeseriesMultipleMetersAllowed() {
	ctx := s.GetContext()
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 3, nil)
	s.insertUsage(ctx, s.maxMeter.ID, s.now, 9, nil)

	def := s.timeseriesDef(s.sumMeter.ID)
	def.Filters = nil // no meter_id filter -> both meters

	res, err := s.svc.ExecuteView(ctx, def, s.drVars())
	s.NoError(err)
	s.Require().NotNil(res)
	s.Require().Len(res.Rows, 2) // one point per meter
}

// TestExecuteView_TimeseriesRejectsSortAndLimit proves timeseries Sort/Limit
// now fail loud at validation rather than being silently ignored: a request
// carrying either must be rejected, so a client never believes a Limit was
// honored when it was not.
func (s *AnalyticsServiceSuite) TestExecuteView_TimeseriesRejectsSortAndLimit() {
	ctx := s.GetContext()

	def := s.timeseriesDef(s.sumMeter.ID)
	def.Limit = 1
	_, err := s.svc.ExecuteView(ctx, def, s.drVars())
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))

	def = s.timeseriesDef(s.sumMeter.ID)
	def.Sort = []*analytics.SortSpec{{Field: "usage_quantity", Dir: types.SortDirectionDesc}}
	_, err = s.svc.ExecuteView(ctx, def, s.drVars())
	s.Require().Error(err)
	s.True(ierr.IsValidation(err))
}

// ---------------------------------------------------------------------------
// View CRUD
// ---------------------------------------------------------------------------

func (s *AnalyticsServiceSuite) TestCreateView_QueryView_RoundTrip() {
	ctx := s.GetContext()
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 3, map[string]interface{}{"region": "us"})
	s.insertUsage(ctx, s.sumMeter.ID, s.now, 5, map[string]interface{}{"region": "eu"})

	view := &analytics.View{
		Name:       "region breakdown",
		Definition: s.breakdownDef(),
	}
	s.NoError(s.svc.CreateView(ctx, view))
	s.NotEmpty(view.ID, "CreateView should assign an ID")

	vars := s.drVars()
	vars["meter"] = []string{s.sumMeter.ID}

	direct, err := s.svc.ExecuteView(ctx, s.breakdownDef(), vars)
	s.NoError(err)

	viaView, err := s.svc.QueryView(ctx, view.ID, vars)
	s.NoError(err)
	s.Equal(direct, viaView)
}

// TestCreateView_DefaultsVersionToOne proves the Version invariant now lives in
// the service: a caller that leaves Version unset (zero value) still gets a
// persisted view with Version == 1, regardless of what the handler does.
func (s *AnalyticsServiceSuite) TestCreateView_DefaultsVersionToOne() {
	ctx := s.GetContext()
	view := &analytics.View{
		Name:       "no version set",
		Definition: s.breakdownDef(),
	}
	s.Equal(0, view.Version, "precondition: caller left Version unset")

	s.NoError(s.svc.CreateView(ctx, view))
	s.Equal(1, view.Version, "CreateView should default Version to 1 on the in-memory value")

	stored, err := s.GetStores().AnalyticsViewRepo.Get(ctx, view.ID)
	s.NoError(err)
	s.Equal(1, stored.Version, "CreateView should persist Version == 1")
}

func (s *AnalyticsServiceSuite) TestCreateView_RejectsInvalidDefinition() {
	ctx := s.GetContext()
	view := &analytics.View{
		Name:       "invalid",
		Definition: &analytics.ViewDefinition{Shape: types.ShapeBreakdown}, // no metrics
	}
	err := s.svc.CreateView(ctx, view)
	s.Error(err)
	s.True(ierr.IsValidation(err))
}

func (s *AnalyticsServiceSuite) TestQueryView_CrossTenantDoesNotLeak() {
	ctx := s.GetContext()
	view := &analytics.View{
		Name:       "tenant-a view",
		Definition: s.breakdownDef(),
	}
	s.NoError(s.svc.CreateView(ctx, view))

	otherTenantCtx := types.SetTenantID(ctx, "other_tenant_xyz")

	vars := s.drVars()
	vars["meter"] = []string{s.sumMeter.ID}

	_, err := s.svc.QueryView(otherTenantCtx, view.ID, vars)
	s.Error(err)
	s.True(ierr.IsNotFound(err), "expected not-found error for cross-tenant access, got: %v", err)
}
