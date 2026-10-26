package clickhouse

import (
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

type stubConn struct{ driver.Conn }

func TestForReportRoutesUsageMetrics(t *testing.T) {
	main, usage := &stubConn{}, &stubConn{}
	r := &ReportsRepository{conn: main, source: "main:9000", usageConn: usage, usageSource: "usage:9000"}

	got := r.forReport(customRV(types.MetricEventsVolume, types.ShapeTimeseries))
	assert.Same(t, usage, got.conn)
	assert.Equal(t, "usage:9000", got.source)

	got = r.forReport(customRV(types.MetricInvoiceTotal, types.ShapeScalar))
	assert.Same(t, main, got.conn)
	assert.Equal(t, "main:9000", got.source)

	r.usageConn = nil
	got = r.forReport(customRV(types.MetricEventsVolume, types.ShapeTimeseries))
	assert.Same(t, main, got.conn)
}
