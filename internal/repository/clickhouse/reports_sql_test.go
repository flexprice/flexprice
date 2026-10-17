package clickhouse

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/analytics"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const billTime = "nullIf(coalesce(nullIf(period_start, toDateTime64(0, 6)), issue_date), toDateTime64(0, 6))"

// Range predicates use the raw expression.
const billCol = "coalesce(nullIf(period_start, toDateTime64(0, 6)), issue_date)"

func billingRV(shape types.Shape, dims ...string) *analytics.ResolvedView {
	return &analytics.ResolvedView{
		Family:     types.FamilyBilling,
		Shape:      shape,
		Metrics:    []types.Metric{types.MetricInvoiceTotal},
		Dimensions: dims,
		Time: analytics.TimeSpec{
			From:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			To:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			Grain: types.GrainMonth,
		},
	}
}

func TestBuildBillingQuery_ScopesAndInvariants(t *testing.T) {
	q, err := buildBillingQuery(billingRV(types.ShapeScalar), "reports_db", "tenant_1", "env_1", 1000)
	require.NoError(t, err)
	for _, frag := range []string{
		"FROM reports_db.invoices WHERE",
		"tenant_id = {tenant_id:String}",
		"environment_id = {environment_id:String}",
		"_peerdb_is_deleted = 0",
		"status = 'published'",
		"invoice_status = 'FINALIZED'",
		billCol + " >= toDateTime64({from:String}, 6, 'UTC')",
		billCol + " < toDateTime64({to:String}, 6, 'UTC')",
		"LIMIT 1001",
	} {
		assert.NotContains(t, q.SQL, " FINAL")
		assert.Contains(t, q.SQL, frag)
	}
	assert.Equal(t, "tenant_1", q.Params["tenant_id"])
	assert.Equal(t, "env_1", q.Params["environment_id"])
	assert.Equal(t, "2026-09-01 00:00:00.000000", q.Params["from"])
	assert.Equal(t, "2026-10-01 00:00:00.000000", q.Params["to"])
}

func TestBuildBillingQuery_ScalarForcesCurrency(t *testing.T) {
	q, err := buildBillingQuery(billingRV(types.ShapeScalar), "reports_db", "t", "e", 0)
	require.NoError(t, err)
	require.Len(t, q.Columns, 2)
	assert.Equal(t, analytics.ReportColumn{Name: "currency", Type: types.ColumnTypeString, Role: types.ColumnRoleDimension}, q.Columns[0])
	assert.Equal(t, analytics.ReportColumn{Name: "invoice_total", Type: types.ColumnTypeDecimal, Role: types.ColumnRoleMetric}, q.Columns[1])
	assert.Contains(t, q.SQL, "GROUP BY d_currency ORDER BY d_currency")
	assert.NotContains(t, q.SQL, "LIMIT")
}

func TestBuildBillingQuery_TimeseriesBucketsInUTC(t *testing.T) {
	q, err := buildBillingQuery(billingRV(types.ShapeTimeseries, "customer_id"), "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "toStartOfMonth("+billTime+", 'UTC')")
	assert.Equal(t, []string{"bucket", "customer_id", "currency", "invoice_total"}, columnNames(q))
	assert.Contains(t, q.SQL, "GROUP BY bucket, d_customer_id, d_currency")
	assert.Contains(t, q.SQL, "ORDER BY bucket, d_customer_id, d_currency")
}

func TestBuildBillingQuery_GrainMap(t *testing.T) {
	for grain, fn := range map[types.Grain]string{
		"": "toStartOfDay", types.GrainDay: "toStartOfDay", types.GrainWeek: "toMonday", types.GrainMonth: "toStartOfMonth",
	} {
		rv := billingRV(types.ShapeTimeseries)
		rv.Time.Grain = grain
		q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
		require.NoError(t, err)
		assert.Contains(t, q.SQL, fn+"(nullIf(", "grain %q", grain)
	}
}

func TestBuildBillingQuery_BreakdownOrdersByMeasure(t *testing.T) {
	q, err := buildBillingQuery(billingRV(types.ShapeBreakdown, "currency"), "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"currency", "invoice_total"}, columnNames(q))
	assert.Contains(t, q.SQL, "ORDER BY sum(total) DESC")
}

func TestBuildBillingQuery_TopNPushesLimit(t *testing.T) {
	rv := billingRV(types.ShapeBreakdown, "customer_id")
	rv.Limit = 10
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 1000)
	require.NoError(t, err)
	assert.True(t, q.TopN)
	assert.Contains(t, q.SQL, "ORDER BY sum(total) DESC LIMIT 10")
	assert.NotContains(t, q.SQL, "LIMIT 1001")

	rv.Sort = []*analytics.SortSpec{{Field: "invoice_total", Dir: types.SortDirectionDesc}}
	q, err = buildBillingQuery(rv, "reports_db", "t", "e", 1000)
	require.NoError(t, err)
	assert.True(t, q.TopN)

	rv.Limit = 5000
	q, err = buildBillingQuery(rv, "reports_db", "t", "e", 1000)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "LIMIT 1000")
}

func TestBuildBillingQuery_NonTopNKeepsOverflowProbe(t *testing.T) {
	rv := billingRV(types.ShapeBreakdown, "customer_id")
	rv.Limit = 10
	rv.Sort = []*analytics.SortSpec{{Field: "customer_id", Dir: types.SortDirectionAsc}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 1000)
	require.NoError(t, err)
	assert.False(t, q.TopN)
	assert.Contains(t, q.SQL, "LIMIT 1001")

	rv.Sort = []*analytics.SortSpec{{Field: "invoice_total"}}
	q, err = buildBillingQuery(rv, "reports_db", "t", "e", 1000)
	require.NoError(t, err)
	assert.False(t, q.TopN)
}

func TestBuildBillingQuery_FilterValuesAreParams(t *testing.T) {
	rv := billingRV(types.ShapeScalar)
	evil := "x' OR 1=1 --"
	rv.Filters = []*analytics.Filter{{Field: "customer_id", Op: types.IN, Value: []string{"cust_1", evil}}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "customer_id IN ({f0_0:String}, {f0_1:String})")
	assert.NotContains(t, q.SQL, "OR 1=1")
	assert.Equal(t, "cust_1", q.Params["f0_0"])
	assert.Equal(t, evil, q.Params["f0_1"])
}

func TestBuildBillingQuery_CurrencyFilter(t *testing.T) {
	rv := billingRV(types.ShapeScalar)
	rv.Filters = []*analytics.Filter{{Field: "currency", Op: types.EQUAL, Value: []string{"usd"}}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "currency IN ({f0_0:String})")
	assert.Equal(t, "usd", q.Params["f0_0"])
}

func TestBuildBillingQuery_EmptyFilterMatchesNothing(t *testing.T) {
	rv := billingRV(types.ShapeScalar)
	rv.Filters = []*analytics.Filter{{Field: "customer_id", Op: types.IN, Value: nil}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "1 = 0")
}

func TestBuildBillingQuery_RequiresTenantAndEnvironment(t *testing.T) {
	for _, te := range [][2]string{{"", "e"}, {"t", ""}} {
		_, err := buildBillingQuery(billingRV(types.ShapeScalar), "reports_db", te[0], te[1], 0)
		require.Error(t, err)
		assert.True(t, ierr.IsPermissionDenied(err))
	}
}

func TestBuildBillingQuery_RejectsBadDatabaseIdentifier(t *testing.T) {
	for _, db := range []string{"", "a.b", "db; DROP", "1db"} {
		_, err := buildBillingQuery(billingRV(types.ShapeScalar), db, "t", "e", 0)
		require.Error(t, err, "db %q", db)
	}
}

func TestBuildBillingQuery_RejectsNonAllowlistedInputs(t *testing.T) {
	rv := billingRV(types.ShapeBreakdown, "meter_id")
	_, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.Error(t, err)

	rv = billingRV(types.ShapeScalar)
	rv.Filters = []*analytics.Filter{{Field: "tenant_id", Op: types.EQUAL, Value: []string{"other"}}}
	_, err = buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.Error(t, err)
}

func TestBuildBillingQuery_SelectsAreStringSafe(t *testing.T) {
	q, err := buildBillingQuery(billingRV(types.ShapeTimeseries, "customer_id"), "reports_db", "t", "e", 0)
	require.NoError(t, err)
	selectPart := q.SQL[:strings.Index(q.SQL, " FROM ")]
	assert.Equal(t, len(q.Columns), strings.Count(selectPart, "ifNull("))
	assert.Contains(t, selectPart, "'%Y-%m-%dT%H:%i:%SZ'")
}

func columnNames(q *reportQuery) []string {
	out := make([]string, len(q.Columns))
	for i, c := range q.Columns {
		out[i] = c.Name
	}
	return out
}

func TestBuildBillingQuery_CompareTimeseries(t *testing.T) {
	rv := billingRV(types.ShapeTimeseries)
	rv.Compare = "previous_period"
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Equal(t, []string{"bucket", "currency", "invoice_total", "invoice_total_prev"}, columnNames(q))
	assert.Contains(t, q.SQL, "sumIf(total, ")
	assert.Contains(t, q.SQL, "addMonths("+billTime+", {shift_m:Int32})")
	assert.Contains(t, q.SQL, "toDateTime64({prev_from:String}, 6, 'UTC')")
	tc := billCol
	cur := tc + " >= toDateTime64({from:String}, 6, 'UTC') AND " + tc + " < toDateTime64({to:String}, 6, 'UTC')"
	prev := tc + " >= toDateTime64({prev_from:String}, 6, 'UTC') AND " + tc + " < toDateTime64({prev_to:String}, 6, 'UTC')"
	assert.Contains(t, q.SQL, " AND (("+cur+") OR ("+prev+"))")
	assert.Equal(t, "2026-08-01 00:00:00.000000", q.Params["prev_from"])
	assert.Equal(t, "2026-09-01 00:00:00.000000", q.Params["prev_to"])
	assert.Equal(t, "1", q.Params["shift_m"])
	_, hasShift := q.Params["shift_s"]
	assert.False(t, hasShift)
}

func TestBuildBillingQuery_CompareARPU(t *testing.T) {
	for _, m := range []types.Metric{types.MetricARPU, types.MetricARPUPaid} {
		rv := billingRV(types.ShapeScalar)
		rv.Metrics = []types.Metric{m}
		rv.Compare = "previous_period"
		q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
		require.NoError(t, err)
		assert.Equal(t, []string{"currency", string(m), string(m) + "_prev"}, columnNames(q))
		assert.Equal(t, 4, strings.Count(q.SQL, "uniqExactIf(customer_id, "))
		assert.Contains(t, q.SQL, "sumIf(")
		assert.Contains(t, q.SQL, "{prev_from:String}")
		assert.Contains(t, q.SQL, "{prev_to:String}")
		assert.Contains(t, q.Params, "prev_from")
	}
}

func TestBuildBillingQuery_CompareRollingUsesSeconds(t *testing.T) {
	rv := billingRV(types.ShapeTimeseries)
	rv.Time.From = time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	rv.Time.To = time.Date(2026, 9, 12, 0, 0, 0, 0, time.UTC)
	rv.Compare = "previous_period"
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "addSeconds("+billTime+", {shift_s:Int64})")
	assert.Equal(t, "604800", q.Params["shift_s"])
	_, hasShift := q.Params["shift_m"]
	assert.False(t, hasShift)
}

func TestBuildBillingQuery_CompareYearUsesAddYears(t *testing.T) {
	rv := billingRV(types.ShapeTimeseries)
	rv.Compare = "previous_year"
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "addYears("+billTime+", 1)")
	_, hasShift := q.Params["shift_s"]
	assert.False(t, hasShift)
}

func TestBuildBillingQuery_CompareTopNOrdersByCurrent(t *testing.T) {
	rv := billingRV(types.ShapeBreakdown, "customer_id")
	rv.Compare = "previous_period"
	rv.Limit = 5
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 1000)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "ORDER BY sumIf(total, ")
	assert.Contains(t, q.SQL, " DESC LIMIT 5")
	assert.True(t, q.TopN)
}

func TestBuildBillingQuery_NoCompareUnchanged(t *testing.T) {
	q, err := buildBillingQuery(billingRV(types.ShapeScalar), "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.NotContains(t, q.SQL, "sumIf(")
	_, ok := q.Params["prev_from"]
	assert.False(t, ok)
}

func TestBuildBillingQuery_RejectsBadCompare(t *testing.T) {
	rv := billingRV(types.ShapeScalar)
	rv.Compare = "last_decade"
	_, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestBuildBillingQuery_SnapshotHasNoTimeWindow(t *testing.T) {
	rv := billingRV(types.ShapeScalar)
	rv.Metrics = []types.Metric{types.MetricTotalReceivables}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.NotContains(t, q.SQL, "{from:String}")
	assert.NotContains(t, q.SQL, "{to:String}")

	rv.Compare = "previous_period"
	_, err = buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}

func TestBuildBillingQuery_ExpressionDimension(t *testing.T) {
	rv := billingRV(types.ShapeBreakdown, "aging_bucket")
	rv.Metrics = []types.Metric{types.MetricReceivablesAging}
	rv.Filters = []*analytics.Filter{{Field: "aging_bucket", Op: types.EQUAL, Value: []string{"1-30"}}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "ifNull(toString("+agingBucketExpr+"), '') AS d_aging_bucket")
	assert.Contains(t, q.SQL, "GROUP BY d_aging_bucket")
	assert.Contains(t, q.SQL, agingBucketExpr+" IN ({f0_0:String})")
}

func TestBuildBillingQuery_CustomBuildHook(t *testing.T) {
	var got buildInput
	withSQL(t, types.MetricActiveCustomers, &billingSQL{
		Build: func(in buildInput) (*reportQuery, error) {
			got = in
			return &reportQuery{SQL: "SELECT 1 FROM " + in.DB + ".x i WHERE " + in.Scope("i") + " AND " + in.TimeCond("i.created_at"), Params: in.Params}, nil
		},
	})
	rv := billingRV(types.ShapeScalar)
	rv.Metrics = []types.Metric{types.MetricActiveCustomers}
	q, err := buildBillingQuery(rv, "reports_db", "tenant_1", "env_1", 0)
	require.NoError(t, err)
	assert.Equal(t, "tenant_1", got.Params["tenant_id"])
	assert.Contains(t, q.SQL, "i.tenant_id = {tenant_id:String} AND i.environment_id = {environment_id:String} AND i._peerdb_is_deleted = 0 AND i.status = 'published'")
	assert.Contains(t, q.SQL, "i.created_at >= toDateTime64({from:String}, 6, 'UTC')")

	_, err = buildBillingQuery(rv, "reports_db", "", "env_1", 0)
	require.Error(t, err)
	assert.True(t, ierr.IsPermissionDenied(err))
}

func TestBuildBillingQuery_InvoiceTotalIssued(t *testing.T) {
	rv := billingRV(types.ShapeTimeseries)
	rv.Metrics = []types.Metric{types.MetricInvoiceTotalIssued}
	rv.Compare = "previous_period"
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "issue_date >= toDateTime64({from:String}, 6, 'UTC')")
	assert.Contains(t, q.SQL, "invoice_status = 'FINALIZED'")
	assert.NotContains(t, q.SQL, "period_start")
	assert.Equal(t, []string{"bucket", "currency", "invoice_total_issued", "invoice_total_issued_prev"}, columnNames(q))

	base, err := buildBillingQuery(billingRV(types.ShapeTimeseries), "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, base.SQL, billTime)
	assert.NotContains(t, base.SQL, " issue_date >=")
}

func TestBuildBillingQuery_ARPUBilled(t *testing.T) {
	rv := billingRV(types.ShapeTimeseries)
	rv.Metrics = []types.Metric{types.MetricARPU}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "sum(total) / uniqExact(customer_id)")
	assert.Contains(t, q.SQL, "invoice_status = 'FINALIZED'")
	assert.Contains(t, q.SQL, "status = 'published'")
	assert.Contains(t, q.SQL, "_peerdb_is_deleted = 0")
	assert.Contains(t, q.SQL, invoiceBillingTime(""))
	assert.NotContains(t, q.SQL, "paid_at")
	assert.NotContains(t, q.SQL, "amount_paid")
}
