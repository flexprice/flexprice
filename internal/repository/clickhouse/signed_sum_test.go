package clickhouse

import (
	"testing"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestSignedSum_AggregatesAsSum(t *testing.T) {
	assert.IsType(t, &MeterUsageSumAggregator{}, GetMeterUsageAggregator(types.AggregationSignedSum))
	assert.IsType(t, &SumAggregator{}, GetAggregator(types.AggregationSignedSum))

	columns := buildMeterUsageAggregationColumns([]types.AggregationType{types.AggregationSignedSum})
	assert.Equal(t, "SUM(qty_total) AS total_usage", columns[0])
}
