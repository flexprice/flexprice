package clickhouse

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

func TestDedupedFeatureUsageFrom_IncludesDistinctOnAndVersionOrder(t *testing.T) {
	r := &FeatureUsageRepository{}

	sql := r.dedupedFeatureUsageFrom("WHERE tenant_id = ?")

	if !strings.Contains(sql, "SELECT DISTINCT ON ("+r.getDeduplicationKey()+")") {
		t.Fatalf("expected DISTINCT ON dedup key, got: %s", sql)
	}
	if !strings.Contains(sql, "ORDER BY "+r.getDeduplicationOrderBy()) {
		t.Fatalf("expected ORDER BY with version DESC, got: %s", sql)
	}
}

func TestGetWindowedQuery_UsesDedupSubquery(t *testing.T) {
	r := &FeatureUsageRepository{}

	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_123")
	ctx = types.SetEnvironmentID(ctx, "env_123")

	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	params := &events.FeatureUsageParams{
		UsageParams: &events.UsageParams{
			WindowSize:         types.WindowSizeHour,
			AggregationType:    types.AggregationMax,
			StartTime:          start,
			EndTime:            end,
			ExternalCustomerID: "ext_cust_1",
			Filters:            map[string][]string{"plan": []string{"pro"}},
		},
		FeatureID: "feature_1",
	}

	sql := r.getWindowedQuery(ctx, params)

	if !strings.Contains(sql, "SELECT DISTINCT ON ("+r.getDeduplicationKey()+")") {
		t.Fatalf("expected windowed query to use DISTINCT ON dedup key, got: %s", sql)
	}
	if !strings.Contains(sql, "ORDER BY "+r.getDeduplicationOrderBy()) {
		t.Fatalf("expected windowed query to order by dedup key + version DESC, got: %s", sql)
	}
}
