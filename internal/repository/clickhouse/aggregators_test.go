package clickhouse

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

func aggregatorTestContext() context.Context {
	ctx := context.Background()
	ctx = context.WithValue(ctx, types.CtxTenantID, "tenant_test")
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, "env_test")
	return ctx
}

func TestBuildFilterConditions_UsesPlaceholdersAndArgs(t *testing.T) {
	clause, args := buildFilterConditions(map[string][]string{
		"plan":   {"pro", "team"},
		"region": {"us"},
	})

	assert.Contains(t, clause, "JSONExtractString(properties, ?) IN (?,?)")
	assert.Contains(t, clause, "JSONExtractString(properties, ?) = ?")
	assert.Contains(t, clause, "AND")
	assert.Equal(t, 5, len(args))
	assert.Contains(t, args, "plan")
	assert.Contains(t, args, "pro")
	assert.Contains(t, args, "team")
	assert.Contains(t, args, "region")
	assert.Contains(t, args, "us")
}

func TestCountAggregator_InjectionPayloadsStayInArgs(t *testing.T) {
	payload := "x' OR 1=1 --"
	ctx := aggregatorTestContext()
	agg := &CountAggregator{}

	query, args := agg.GetQuery(ctx, &events.UsageParams{
		EventName:          payload,
		ExternalCustomerID: payload,
		CustomerID:         payload,
		StartTime:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:            time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		Filters: map[string][]string{
			"plan": {payload},
		},
	})

	assert.NotContains(t, query, payload)
	assert.Contains(t, query, "PREWHERE tenant_id = ?")
	assert.Contains(t, query, "AND environment_id = ?")
	assert.Contains(t, query, "AND event_name = ?")
	assert.Contains(t, query, "AND external_customer_id = ?")
	assert.Contains(t, query, "AND customer_id = ?")
	assert.Contains(t, query, "JSONExtractString(properties, ?) = ?")
	assert.Contains(t, query, "timestamp >= ?")
	assert.Contains(t, query, "timestamp < ?")
	assert.Contains(t, args, payload)
}
