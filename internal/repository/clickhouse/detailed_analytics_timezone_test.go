package clickhouse

import (
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func detailedTZParams(tz string) *events.MeterUsageDetailedAnalyticsParams {
	return &events.MeterUsageDetailedAnalyticsParams{
		TenantID:           "t1",
		EnvironmentID:      "e1",
		ExternalCustomerID: "cust-1",
		StartTime:          time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC),
		EndTime:            time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
		WindowSize:         types.WindowSizeDay,
		AggregationTypes:   []types.AggregationType{types.AggregationSum},
		Timezone:           tz,
	}
}

// The detailed-analytics time-series buckets in params.Timezone (auto-derived
// server-side from the customer). A non-UTC tz must localize the window; an empty
// tz must bucket in UTC.
func TestBuildDetailedPointsQuery_Timezone(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()
	result := &events.MeterUsageDetailedResult{}

	ist, _ := qb.BuildDetailedPointsQuery(detailedTZParams("Asia/Kolkata"), result, nil)
	assert.Contains(t, ist, "toStartOfDay(timestamp, 'Asia/Kolkata')",
		"non-UTC customer timezone must localize the day bucket")

	utc, _ := qb.BuildDetailedPointsQuery(detailedTZParams(""), result, nil)
	assert.NotContains(t, utc, "Asia/Kolkata", "empty tz must not localize")
	assert.True(t, strings.Contains(utc, "toStartOfDay"), "empty tz must still bucket by day (UTC)")
}
