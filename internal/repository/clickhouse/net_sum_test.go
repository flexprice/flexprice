package clickhouse

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestNetSum_AggregatesAsSum(t *testing.T) {
	assert.IsType(t, &MeterUsageSumAggregator{}, GetMeterUsageAggregator(types.AggregationNetSum))
	assert.IsType(t, &SumAggregator{}, GetAggregator(types.AggregationNetSum))

	columns := buildMeterUsageAggregationColumns([]types.AggregationType{types.AggregationNetSum})
	assert.Equal(t, "SUM(qty_total) AS total_usage", columns[0])
}
