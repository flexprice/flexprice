package clickhouse

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSelfBillingEventCountQuery(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{"hour_bucket_in_utc", "toStartOfHour(ingested_at, 'UTC') AS hour"},
		{"distinct_event_ids", "uniqExact(id)"},
		{"ingest_window_lower_bound", "ingested_at >= ?"},
		{"ingest_window_upper_bound", "ingested_at < ?"},
		{"grouped_per_tenant_env_hour", "GROUP BY tenant_id, environment_id, hour"},
		{"memory_bounded", "SETTINGS max_memory_usage = 96636764160"},
		{"late_cutoff_per_event_not_per_run", "AND timestamp >= ingested_at - INTERVAL 30 DAY"},
		{"event_age_bound_prunes_partitions", "AND timestamp >= ?"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, selfBillingEventCountQuery, tt.want)
		})
	}

	assert.Equal(t, 3, strings.Count(selfBillingEventCountQuery, "?"))
	assert.Equal(t, 30*24*time.Hour, selfBillingLateEventHorizon)
}
