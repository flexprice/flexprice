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

func eventsVolumeRV(shape types.Shape, dims ...string) *analytics.ResolvedView {
	return &analytics.ResolvedView{
		Family:     types.FamilyBilling,
		Shape:      shape,
		Metrics:    []types.Metric{types.MetricEventsVolume},
		Dimensions: dims,
		Time: analytics.TimeSpec{
			From:  time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
			To:    time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			Grain: types.GrainWeek,
		},
	}
}

func TestBuildEventsVolume_TimeseriesUngrouped(t *testing.T) {
	q, err := buildBillingQuery(eventsVolumeRV(types.ShapeTimeseries), "reports_db", "tenant_1", "env_1", 1000)
	require.NoError(t, err)
	assert.Equal(t, "SELECT formatDateTime(toMonday(timestamp, 'UTC'), '%Y-%m-%dT%H:%i:%SZ', 'UTC') AS bucket, toString(count()) AS events_volume"+
		" FROM reports_db.meter_usage WHERE tenant_id = {tenant_id:String} AND environment_id = {environment_id:String}"+
		" AND timestamp >= toDateTime({from_ts:String}, 'UTC') AND timestamp < toDateTime({to_ts:String}, 'UTC')"+
		" GROUP BY bucket ORDER BY bucket LIMIT 1001", q.SQL)
	assert.Equal(t, "tenant_1", q.Params["tenant_id"])
	assert.Equal(t, "env_1", q.Params["environment_id"])
	assert.Equal(t, "2026-09-01 00:00:00", q.Params["from_ts"])
	assert.Equal(t, "2026-10-01 00:00:00", q.Params["to_ts"])
	assert.Len(t, q.Columns, 2)
	assert.False(t, q.TopN)
}

func TestBuildEventsVolume_NoFinalNoPeerdbNoStatus(t *testing.T) {
	for _, rv := range []*analytics.ResolvedView{
		eventsVolumeRV(types.ShapeTimeseries),
		eventsVolumeRV(types.ShapeTimeseries, "meter_id"),
		eventsVolumeRV(types.ShapeBreakdown, "meter_id"),
	} {
		q, err := buildBillingQuery(rv, "reports_db", "t", "e", 10)
		require.NoError(t, err)
		assert.NotContains(t, q.SQL, "FINAL")
		assert.NotContains(t, q.SQL, "_peerdb")
		assert.NotContains(t, q.SQL, "status")
		assert.NotContains(t, q.SQL, "DISTINCT")
	}
}

func TestBuildEventsVolume_Shapes(t *testing.T) {
	q, err := buildBillingQuery(eventsVolumeRV(types.ShapeTimeseries, "meter_id"), "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "AS bucket, meter_id, toString(count())")
	assert.Contains(t, q.SQL, "GROUP BY bucket, meter_id ORDER BY bucket, meter_id")
	assert.NotContains(t, q.SQL, "LIMIT")
	require.Len(t, q.Columns, 3)

	q, err = buildBillingQuery(eventsVolumeRV(types.ShapeBreakdown, "meter_id"), "reports_db", "t", "e", 10)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "SELECT meter_id, toString(count()) AS events_volume")
	assert.Contains(t, q.SQL, "GROUP BY meter_id ORDER BY count() DESC, meter_id LIMIT 11")
	assert.NotContains(t, q.SQL, "bucket")
}

func TestBuildEventsVolume_BreakdownTopN(t *testing.T) {
	rv := eventsVolumeRV(types.ShapeBreakdown, "meter_id")
	rv.Limit = 5
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 3)
	require.NoError(t, err)
	assert.True(t, q.TopN)
	assert.Contains(t, q.SQL, " LIMIT 3")
}

func TestBuildEventsVolume_MeterFilterIsParams(t *testing.T) {
	rv := eventsVolumeRV(types.ShapeBreakdown, "meter_id")
	rv.Filters = []*analytics.Filter{{Field: "meter_id", Op: types.IN, Value: []string{"m1", "x' OR '1'='1"}}}
	q, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "meter_id IN ({f0_0:String}, {f0_1:String})")
	assert.NotContains(t, q.SQL, "OR '1'")
	assert.Equal(t, "x' OR '1'='1", q.Params["f0_1"])
}

func TestBuildEventsVolume_Rejects(t *testing.T) {
	rv := eventsVolumeRV(types.ShapeTimeseries)
	rv.Compare = "previous_period"
	_, err := buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.Error(t, err)

	_, err = buildBillingQuery(eventsVolumeRV(types.ShapeScalar), "reports_db", "t", "e", 0)
	require.Error(t, err)

	rv = eventsVolumeRV(types.ShapeBreakdown, "meter_id")
	rv.Filters = []*analytics.Filter{{Field: "currency", Op: types.EQUAL, Value: []string{"usd"}}}
	_, err = buildBillingQuery(rv, "reports_db", "t", "e", 0)
	require.Error(t, err)
}

func TestBuildEventsVolume_UsageDatabase(t *testing.T) {
	rv := eventsVolumeRV(types.ShapeTimeseries)
	q, err := buildBillingQueryWithUsage(rv, "reports_db", "usage_db", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, " FROM usage_db.meter_usage ")

	q, err = buildBillingQueryWithUsage(rv, "reports_db", "", "t", "e", 0)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, " FROM reports_db.meter_usage ")

	_, err = buildBillingQueryWithUsage(rv, "reports_db", "bad; DROP", "t", "e", 0)
	require.Error(t, err)
}

func TestBuildEventsVolume_ColumnsAndSQL(t *testing.T) {
	cases := []struct {
		shape types.Shape
		grain types.Grain
		dims  []string
		cols  []string
		want  []string
		not   []string
	}{
		{types.ShapeTimeseries, types.GrainDay, nil, []string{"bucket", "events_volume"}, []string{"GROUP BY bucket ORDER BY bucket"}, []string{"meter_id"}},
		{types.ShapeTimeseries, types.GrainDay, []string{"meter_id"}, []string{"bucket", "meter_id", "events_volume"}, []string{"GROUP BY bucket, meter_id ORDER BY bucket, meter_id"}, nil},
		{types.ShapeBreakdown, "", []string{"meter_id"}, []string{"meter_id", "events_volume"}, []string{"GROUP BY meter_id ORDER BY count() DESC, meter_id"}, nil},
	}
	for _, c := range cases {
		rv := eventsVolumeRV(c.shape, c.dims...)
		rv.Time.Grain = c.grain
		q, err := buildBillingQuery(rv, "reports_db", "t", "e", 500)
		require.NoError(t, err, c.shape)
		names := []string{}
		for _, col := range q.Columns {
			names = append(names, col.Name)
		}
		assert.Equal(t, c.cols, names, c.shape)
		assert.Contains(t, q.SQL, "FROM reports_db.meter_usage WHERE")
		assert.NotContains(t, q.SQL, "FINAL")
		assert.NotContains(t, q.SQL, "_peerdb_is_deleted")
		for _, f := range c.want {
			assert.Contains(t, q.SQL, f)
		}
		for _, f := range c.not {
			assert.NotContains(t, q.SQL[:strings.Index(q.SQL, " FROM")], f)
		}
	}
}

func TestBuildEventsVolume_RowCap(t *testing.T) {
	q, err := buildBillingQuery(eventsVolumeRV(types.ShapeBreakdown, "meter_id"), "reports_db", "t", "e", 1)
	require.NoError(t, err)
	assert.Contains(t, q.SQL, "LIMIT 2")
	_, err = reportResult(q, [][]string{{"a", "1"}, {"b", "2"}}, 1)
	require.Error(t, err)
	assert.True(t, ierr.IsValidation(err))
}
