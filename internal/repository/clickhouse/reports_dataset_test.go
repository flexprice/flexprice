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

const joinScope = "FINAL WHERE tenant_id = {tenant_id:String} AND environment_id = {environment_id:String} AND _peerdb_is_deleted = 0 AND status = 'published'"

func dsTimeField(ds *analytics.Dataset) string {
	for _, f := range ds.Fields {
		if f.Kind == analytics.FieldKindTime {
			return f.Key
		}
	}
	return ""
}

func dsQuery(key string, measures []*analytics.Measure, dims []string, filters []*analytics.Filter, grain types.Grain, compare string) *analytics.DatasetQuery {
	ds, _ := analytics.LookupDataset(key)
	return &analytics.DatasetQuery{
		Def: &analytics.ViewDefinition{
			Family:     types.FamilyDataset,
			Dataset:    key,
			Measures:   measures,
			Dimensions: dims,
			Filters:    filters,
			Time:       analytics.TimeSpecRaw{Range: "last_7_days", Grain: grain, Field: dsTimeField(ds)},
			Compare:    compare,
		},
		From:    time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		To:      time.Date(2026, 9, 8, 0, 0, 0, 0, time.UTC),
		Compare: compare,
	}
}

func validValue(f analytics.DatasetField) string {
	switch {
	case len(f.Enum) > 0:
		return f.Enum[0]
	case f.Kind == analytics.FieldKindNumber:
		return "10"
	case f.Kind == analytics.FieldKindBool:
		return "true"
	}
	return "x"
}

func buildDS(t *testing.T, q *analytics.DatasetQuery) *reportQuery {
	t.Helper()
	rq, err := buildDatasetQuery(q, "reports_db", "usage_db", "tenant_1", "env_1", 1000)
	require.NoError(t, err)
	return rq
}

func assertDatasetScoped(t *testing.T, ds *analytics.Dataset, sql string) {
	t.Helper()
	assert.Equal(t, strings.Count(sql, "LEFT JOIN"), strings.Count(sql, joinScope), "every join subquery scoped: %s", sql)
	a := ds.Alias + "."
	assert.Contains(t, sql, a+"tenant_id = {tenant_id:String} AND "+a+"environment_id = {environment_id:String}")
	if ds.Usage {
		assert.Contains(t, sql, " FROM usage_db.meter_usage AS u ")
		assert.NotContains(t, sql, "meter_usage AS u FINAL")
		return
	}
	assert.Contains(t, sql, " FROM reports_db."+ds.Table+" AS "+ds.Alias+" FINAL")
	assert.Contains(t, sql, a+"_peerdb_is_deleted = 0 AND "+a+"status = 'published'")
}

func TestBuildDatasetQuery_EveryDataset(t *testing.T) {
	for _, ds := range analytics.Datasets() {
		t.Run(ds.Key, func(t *testing.T) {
			measures := []*analytics.Measure{{Agg: "count"}}
			var dim, num string
			var flt *analytics.Filter
			for _, f := range ds.Fields {
				if f.Group && dim == "" {
					dim = f.Key
					measures = append(measures, &analytics.Measure{Agg: "count_distinct", Field: f.Key})
				}
				if f.Measure && f.Kind == analytics.FieldKindNumber && num == "" {
					num = f.Key
					for _, agg := range []string{"sum", "avg", "min", "max"} {
						measures = append(measures, &analytics.Measure{Agg: agg, Field: f.Key})
					}
				}
				if f.Filter && flt == nil {
					flt = &analytics.Filter{Field: f.Key, Op: types.EQUAL, Value: []string{validValue(f)}}
				}
			}
			rq := buildDS(t, dsQuery(ds.Key, measures, []string{dim}, []*analytics.Filter{flt}, "", ""))
			assertDatasetScoped(t, &ds, rq.SQL)
			assert.Contains(t, rq.SQL, "toString(count()), '') AS m0")
			assert.Contains(t, rq.SQL, "uniqExact(")
			if num != "" {
				for _, fn := range []string{"sum(", "avg(", "min(", "max("} {
					assert.Contains(t, rq.SQL, fn)
				}
			}
			assert.Contains(t, rq.SQL, "ORDER BY count() DESC")
			assert.Equal(t, "d_"+dim, "d_"+rq.Columns[0].Name)
		})
	}
}

func TestBuildDatasetQuery_EveryCatalogField(t *testing.T) {
	for _, ds := range analytics.Datasets() {
		joined := map[string]bool{}
		for _, f := range ds.Fields {
			var qs []*analytics.DatasetQuery
			if f.Group {
				qs = append(qs, dsQuery(ds.Key, []*analytics.Measure{{Agg: "count"}}, []string{f.Key}, nil, "", ""))
			}
			if f.Filter {
				qs = append(qs, dsQuery(ds.Key, []*analytics.Measure{{Agg: "count"}}, nil,
					[]*analytics.Filter{{Field: f.Key, Op: types.IN, Value: []string{validValue(f)}}}, "", ""))
			}
			if f.Measure {
				aggs := []string{"count_distinct"}
				if f.Kind == analytics.FieldKindNumber {
					aggs = append(aggs, "sum", "avg", "min", "max")
				}
				for _, agg := range aggs {
					qs = append(qs, dsQuery(ds.Key, []*analytics.Measure{{Agg: agg, Field: f.Key}}, nil, nil, types.GrainDay, ""))
				}
			}
			if f.Kind == analytics.FieldKindTime {
				q := dsQuery(ds.Key, []*analytics.Measure{{Agg: "count"}}, nil, nil, types.GrainWeek, "previous_period")
				q.Def.Time.Field = f.Key
				qs = append(qs, q)
			}
			for _, q := range qs {
				rq, err := buildDatasetQuery(q, "reports_db", "usage_db", "tenant_1", "env_1", 1000)
				require.NoError(t, err, "%s.%s", ds.Key, f.Key)
				assertDatasetScoped(t, &ds, rq.SQL)
				assert.Contains(t, rq.SQL, f.Expr, "%s.%s", ds.Key, f.Key)
				for _, j := range ds.Joins {
					if strings.Contains(rq.SQL, ") AS "+j.Alias+" ON ") {
						joined[j.Alias] = true
					}
				}
			}
		}
		for _, j := range ds.Joins {
			assert.True(t, joined[j.Alias], "%s join %s never used", ds.Key, j.Alias)
		}
	}
}

func TestBuildDatasetQuery_UsageCustomerJoinManyToOne(t *testing.T) {
	rq := buildDS(t, dsQuery("usage", []*analytics.Measure{{Agg: "sum", Field: "quantity"}}, []string{"customer"}, nil, "", ""))
	assert.Contains(t, rq.SQL, "LEFT JOIN (SELECT external_id, any(id) AS id, any(name) AS name FROM reports_db.customers "+joinScope+" GROUP BY external_id) AS c ON c.external_id = u.external_customer_id")
	assert.Contains(t, rq.SQL, "ifNull(toString(any(c.name)), '') AS d_customer_label")
	assert.NotContains(t, rq.SQL, "d_currency")
	assert.NotContains(t, rq.SQL, "nullIf(u.timestamp")
}

func TestBuildDatasetQuery_JoinsOnlyWhenReferenced(t *testing.T) {
	rq := buildDS(t, dsQuery("invoices", []*analytics.Measure{{Agg: "count"}}, nil, nil, "", ""))
	assert.NotContains(t, rq.SQL, "LEFT JOIN")
	rq = buildDS(t, dsQuery("invoices", []*analytics.Measure{{Agg: "count"}}, []string{"customer"}, nil, "", ""))
	assert.Contains(t, rq.SQL, "LEFT JOIN (SELECT id, name, external_id, address_country FROM reports_db.customers "+joinScope+") AS c ON c.id = i.customer_id")
	assert.NotContains(t, rq.SQL, "subscriptions")
}

func TestBuildDatasetQuery_MoneyAddsCurrencyOnce(t *testing.T) {
	sum := []*analytics.Measure{{Agg: "sum", Field: "total"}}
	rq := buildDS(t, dsQuery("invoices", sum, []string{"invoice_status"}, nil, "", ""))
	assert.Equal(t, 1, strings.Count(rq.SQL, "AS d_currency"))
	assert.Contains(t, rq.SQL, "GROUP BY d_invoice_status, d_currency")
	assert.Equal(t, []string{"invoice_status", "currency", "m0"}, colNames(rq.Columns))

	rq = buildDS(t, dsQuery("invoices", sum, []string{"currency"}, nil, "", ""))
	assert.Equal(t, 1, strings.Count(rq.SQL, "AS d_currency"))

	rq = buildDS(t, dsQuery("invoices", []*analytics.Measure{{Agg: "count"}}, []string{"invoice_status"}, nil, "", ""))
	assert.NotContains(t, rq.SQL, "d_currency")
}

func colNames(cols []analytics.ReportColumn) []string {
	out := make([]string, len(cols))
	for i, c := range cols {
		out[i] = c.Name
	}
	return out
}

func TestBuildDatasetQuery_InjectionOnlyInParams(t *testing.T) {
	evil := "' OR 1=1 --"
	rq := buildDS(t, dsQuery("invoices", []*analytics.Measure{{Agg: "count"}}, nil,
		[]*analytics.Filter{{Field: "billing_reason", Op: types.NOT_IN, Value: []string{"a", evil}}}, "", ""))
	assert.NotContains(t, rq.SQL, "OR 1=1")
	assert.Contains(t, rq.SQL, "i.billing_reason NOT IN ({f0_0:String}, {f0_1:String})")
	assert.Equal(t, evil, rq.Params["f0_1"])
}

func TestBuildDatasetQuery_FilterOps(t *testing.T) {
	cases := []struct {
		op   types.FilterOperatorType
		vals []string
		want string
	}{
		{types.EQUAL, []string{"5"}, "i.total = {f0_0:Decimal(38, 9)}"},
		{types.GREATER_THAN, []string{"5"}, "i.total > {f0_0:Decimal(38, 9)}"},
		{types.GREATER_THAN_EQUAL, []string{"5"}, "i.total >= {f0_0:Decimal(38, 9)}"},
		{types.LESS_THAN, []string{"5"}, "i.total < {f0_0:Decimal(38, 9)}"},
		{analytics.FilterOpLTE, []string{"5"}, "i.total <= {f0_0:Decimal(38, 9)}"},
		{types.IN, []string{"5", "6"}, "i.total IN ({f0_0:Decimal(38, 9)}, {f0_1:Decimal(38, 9)})"},
		{analytics.FilterOpBetween, []string{"1", "9"}, "(i.total >= {f0_0:Decimal(38, 9)} AND i.total <= {f0_1:Decimal(38, 9)})"},
	}
	for _, c := range cases {
		rq := buildDS(t, dsQuery("invoices", []*analytics.Measure{{Agg: "count"}}, nil,
			[]*analytics.Filter{{Field: "total", Op: c.op, Value: c.vals}}, "", ""))
		assert.Contains(t, rq.SQL, c.want, string(c.op))
	}
}

func TestBuildDatasetQuery_Compare(t *testing.T) {
	q := dsQuery("invoices", []*analytics.Measure{{Agg: "count"}, {Agg: "sum", Field: "total"}}, nil, nil, types.GrainDay, "previous_period")
	q.From, q.To = q.From.AddDate(0, 0, 1), q.To.AddDate(0, 0, 1)
	rq := buildDS(t, q)
	cur := "nullIf(coalesce(nullIf(i.period_start, toDateTime64(0, 6)), i.issue_date), toDateTime64(0, 6))"
	assert.Contains(t, rq.SQL, "ifNull(toString(countIf("+cur+" >= toDateTime64({from:String}, 6, 'UTC')")
	assert.Contains(t, rq.SQL, "AS m1_prev")
	assert.Contains(t, rq.SQL, "sumIf(i.total, "+cur+" >= toDateTime64({prev_from:String}, 6, 'UTC')")
	assert.Contains(t, rq.SQL, "addSeconds("+cur+", {shift_s:Int64})")
	assert.Equal(t, "2026-08-26 00:00:00.000000", rq.Params["prev_from"])
	assert.Equal(t, "2026-09-02 00:00:00.000000", rq.Params["prev_to"])
	assert.Equal(t, "604800", rq.Params["shift_s"])
	assert.Equal(t, []string{"bucket", "currency", "m0", "m1", "m0_prev", "m1_prev"}, colNames(rq.Columns))
	assert.Contains(t, rq.SQL, "ORDER BY bucket, d_currency LIMIT 1001")
}

func TestBuildDatasetQuery_TopN(t *testing.T) {
	q := dsQuery("invoices", []*analytics.Measure{{Agg: "count"}}, []string{"invoice_status"}, nil, "", "")
	q.Def.Limit = 5
	rq := buildDS(t, q)
	assert.True(t, rq.TopN)
	assert.True(t, strings.HasSuffix(rq.SQL, "ORDER BY count() DESC LIMIT 5"))

	q.Def.Sort = []*analytics.SortSpec{{Field: "invoice_status", Dir: types.SortDirectionAsc}}
	rq = buildDS(t, q)
	assert.False(t, rq.TopN)
	assert.True(t, strings.HasSuffix(rq.SQL, "LIMIT 1001"))
}

func TestBuildDatasetQuery_Errors(t *testing.T) {
	q := dsQuery("invoices", []*analytics.Measure{{Agg: "count"}}, nil, nil, "", "")
	_, err := buildDatasetQuery(q, "reports_db", "", "", "env_1", 10)
	assert.True(t, ierr.IsPermissionDenied(err))
	_, err = buildDatasetQuery(q, "reports_db", "", "tenant_1", "", 10)
	assert.True(t, ierr.IsPermissionDenied(err))
	_, err = buildDatasetQuery(q, "bad-db;", "", "tenant_1", "env_1", 10)
	assert.True(t, ierr.IsSystem(err))
	_, err = buildDatasetQuery(q, "reports_db", "bad db", "tenant_1", "env_1", 10)
	assert.True(t, ierr.IsSystem(err))

	u := dsQuery("usage", []*analytics.Measure{{Agg: "count"}}, nil, nil, "", "")
	u.To = u.From.AddDate(0, 0, 8)
	_, err = buildDatasetQuery(u, "reports_db", "", "tenant_1", "env_1", 10)
	assert.True(t, ierr.IsValidation(err))

	bad := dsQuery("invoices", []*analytics.Measure{{Agg: "sum", Field: "invoice_status"}}, nil, nil, "", "")
	_, err = buildDatasetQuery(bad, "reports_db", "", "tenant_1", "env_1", 10)
	assert.True(t, ierr.IsValidation(err))
}
