package clickhouse

import (
	"context"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// maliciousProperty and maliciousValue mirror the live-verified staging PoC:
// POST /v1/events/usage with filters: {"x') OR 1=1 -- ": ["a"]} caused a
// 500 database_error because the property name reached ClickHouse unescaped.
const (
	maliciousProperty = "x') OR 1=1 -- "
	maliciousValue    = "y') OR 1=1 -- "
)

func testCtx() context.Context {
	ctx := context.Background()
	ctx = types.SetTenantID(ctx, "tenant_injection_test")
	ctx = types.SetEnvironmentID(ctx, "env_injection_test")
	return ctx
}

func baseUsageParams() *events.UsageParams {
	return &events.UsageParams{
		EventName:    "api_calls",
		PropertyName: "value",
		StartTime:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:      time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC),
	}
}

// assertNoRawInjection asserts the raw malicious strings never appear literally
// in the query text, and that they DO appear in args (bound via `?`).
func assertNoRawInjection(t *testing.T, query string, args []interface{}) {
	t.Helper()
	assert.NotContains(t, query, "OR 1=1", "malicious payload leaked into SQL string")
	assert.NotContains(t, query, maliciousProperty, "raw filter property leaked into SQL string")
	assert.NotContains(t, query, maliciousValue, "raw filter value leaked into SQL string")
	assert.Contains(t, args, maliciousProperty, "filter property must be bound as an arg")
	assert.Contains(t, args, maliciousValue, "filter value must be bound as an arg")
}

func TestBuildFilterConditions(t *testing.T) {
	cases := []struct {
		name     string
		filters  map[string][]string
		validate func(*testing.T, string, []interface{})
	}{
		{
			name:    "Parameterized",
			filters: map[string][]string{maliciousProperty: {"a"}},
			validate: func(t *testing.T, clause string, args []interface{}) {
				assert.NotContains(t, clause, "OR 1=1", "raw filter value leaked into SQL string")
				assert.NotContains(t, clause, "x')", "raw property name leaked into SQL string")
				assert.Contains(t, clause, "JSONExtractString(properties, ?)")
				assert.Contains(t, args, maliciousProperty)
				assert.Contains(t, args, "a")
			},
		},
		{
			name:    "MultiValue",
			filters: map[string][]string{"region": {"us", "eu"}},
			validate: func(t *testing.T, clause string, args []interface{}) {
				assert.Contains(t, clause, "JSONExtractString(properties, ?) IN (?,?)")
				assert.Equal(t, []interface{}{"region", "us", "eu"}, args)
			},
		},
		{
			name:    "Empty",
			filters: nil,
			validate: func(t *testing.T, clause string, args []interface{}) {
				assert.Equal(t, "", clause)
				assert.Nil(t, args)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clause, args := buildFilterConditions(tc.filters)
			tc.validate(t, clause, args)
		})
	}
}

func TestBuildUsageEventCustomerFilters_Parameterized(t *testing.T) {
	params := &events.UsageParams{
		ExternalCustomerID: maliciousProperty,
		CustomerID:         maliciousValue,
	}
	externalFilter, customerFilter, args := buildUsageEventCustomerFilters(params)

	assert.Equal(t, "AND external_customer_id = ?", externalFilter)
	assert.Equal(t, "AND customer_id = ?", customerFilter)
	assert.NotContains(t, externalFilter, "OR 1=1")
	assert.NotContains(t, customerFilter, "OR 1=1")
	assert.Contains(t, args, maliciousProperty)
	assert.Contains(t, args, maliciousValue)
}

func TestBuildUsageEventCustomerFilters_MultiExternalIDs(t *testing.T) {
	params := &events.UsageParams{
		ExternalCustomerIDs: []string{maliciousProperty, "cust-2"},
	}
	externalFilter, _, args := buildUsageEventCustomerFilters(params)

	assert.Contains(t, externalFilter, "IN (?, ?)")
	assert.NotContains(t, externalFilter, maliciousProperty)
	assert.Equal(t, []interface{}{maliciousProperty, "cust-2"}, args)
}

// --- Aggregator.GetQuery injection tests ---
// Each aggregator's GetQuery must never interpolate filter property names/values,
// customer IDs, or event name into the raw query string — they must appear only
// in the returned args slice.

func TestAggregatorGetQuery_NoInjection(t *testing.T) {
	ctx := testCtx()

	aggregators := []events.Aggregator{
		&CountAggregator{},
		&SumAggregator{},
		&AvgAggregator{},
		&CountUniqueAggregator{},
		&LatestAggregator{},
		&SumWithMultiAggregator{},
		&MaxAggregator{},
		&WeightedSumAggregator{},
	}

	for _, agg := range aggregators {
		for _, bucketSize := range []types.WindowSize{"", types.WindowSizeHour} {
			name := string(agg.GetType())
			if bucketSize != "" {
				name += "_windowed"
			}
			t.Run(name, func(t *testing.T) {
				params := baseUsageParams()
				params.BucketSize = bucketSize
				params.ExternalCustomerID = maliciousProperty
				params.Filters = map[string][]string{maliciousProperty: {maliciousValue}}
				if agg.GetType() == types.AggregationSumWithMultiplier {
					m := decimal.NewFromInt(1)
					params.Multiplier = &m
				}

				query, args := agg.GetQuery(ctx, params)

				assertNoRawInjection(t, query, args)
			})
		}
	}
}

func TestCountAggregator_GetQuery_TenantEnvironmentEventNameBound(t *testing.T) {
	ctx := testCtx()
	params := baseUsageParams()
	params.EventName = maliciousProperty

	agg := &CountAggregator{}
	query, args := agg.GetQuery(ctx, params)

	assert.NotContains(t, query, "OR 1=1")
	assert.NotContains(t, query, maliciousProperty)
	assert.Contains(t, args, maliciousProperty)
	assert.Contains(t, args, "tenant_injection_test")
	assert.Contains(t, args, "env_injection_test")
}
