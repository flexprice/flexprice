package builder

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
)

// define a context with a tenant ID to be used in all tests
var ctx = context.WithValue(context.Background(), types.CtxTenantID, types.DefaultTenantID)

func TestQueryBuilder_WithBaseFilters(t *testing.T) {
	tests := []struct {
		name     string
		params   *events.UsageParams
		wantSQL  string
		wantArgs []interface{}
	}{
		{
			name: "base filters with all params",
			params: &events.UsageParams{
				EventName:          "audio_transcription",
				StartTime:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndTime:            time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
				CustomerID:         "cust_123",
				ExternalCustomerID: "ext_123",
			},
			wantSQL: "WITH base_events AS (SELECT * FROM (SELECT DISTINCT ON (tenant_id, environment_id, timestamp, id) * FROM events WHERE event_name = ? AND tenant_id = ? AND timestamp >= toDateTime64('2024-01-01 00:00:00.000', 3, 'UTC') AND timestamp < toDateTime64('2024-01-02 00:00:00.000', 3, 'UTC') AND external_customer_id = ? AND customer_id = ? ORDER BY tenant_id, environment_id, timestamp, id DESC))",
			wantArgs: []interface{}{"audio_transcription", "00000000-0000-0000-0000-000000000000", "ext_123", "cust_123"},
		},
		{
			name: "base filters without customer ID",
			params: &events.UsageParams{
				EventName: "api_calls",
				StartTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndTime:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			},
			wantSQL:  "WITH base_events AS (SELECT * FROM (SELECT DISTINCT ON (tenant_id, environment_id, timestamp, id) * FROM events WHERE event_name = ? AND tenant_id = ? AND timestamp >= toDateTime64('2024-01-01 00:00:00.000', 3, 'UTC') AND timestamp < toDateTime64('2024-01-02 00:00:00.000', 3, 'UTC') ORDER BY tenant_id, environment_id, timestamp, id DESC))",
			wantArgs: []interface{}{"api_calls", "00000000-0000-0000-0000-000000000000"},
		},
		{
			// Regression: non-UTC inputs must be converted to UTC clock time and the
			// literal wrapped with an explicit `'UTC'` — without this, ClickHouse
			// reinterprets the naive string in the server's local tz and silently
			// shifts the window.
			name: "non-UTC time inputs are converted to UTC in toDateTime64",
			params: &events.UsageParams{
				EventName: "api_calls",
				// 10:00 in +02:00 == 08:00 UTC
				StartTime: time.Date(2024, 1, 1, 10, 0, 0, 0, time.FixedZone("+02:00", 2*60*60)),
				// 12:30 in -05:00 == 17:30 UTC
				EndTime: time.Date(2024, 1, 2, 12, 30, 0, 0, time.FixedZone("-05:00", -5*60*60)),
			},
			wantSQL:  "WITH base_events AS (SELECT * FROM (SELECT DISTINCT ON (tenant_id, environment_id, timestamp, id) * FROM events WHERE event_name = ? AND tenant_id = ? AND timestamp >= toDateTime64('2024-01-01 08:00:00.000', 3, 'UTC') AND timestamp < toDateTime64('2024-01-02 17:30:00.000', 3, 'UTC') ORDER BY tenant_id, environment_id, timestamp, id DESC))",
			wantArgs: []interface{}{"api_calls", "00000000-0000-0000-0000-000000000000"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, tt.params)
			sql, args := qb.Build()
			sql = strings.ReplaceAll(sql, "\n", "")
			sql = strings.ReplaceAll(sql, "\t", "")
			expected := strings.ReplaceAll(tt.wantSQL, "\n", "")
			expected = strings.ReplaceAll(expected, "\t", "")
			assert.Equal(t, expected, sql)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}

func TestQueryBuilder_WithFilterGroups(t *testing.T) {
	tests := []struct {
		name        string
		meterConfig *meter.Meter
		groups      []events.FilterGroup
		wantCTEs    []string
		wantArgs    []interface{}
	}{
		{
			name: "multiple filter groups with different priorities",
			meterConfig: &meter.Meter{
				EventName: "audio_transcription",
				Filters: []meter.Filter{
					{Key: "test_group", Values: []string{"group_0", "group_1"}},
					{Key: "audio_model", Values: []string{"whisper", "deepgram"}},
				},
			},
			groups: []events.FilterGroup{
				{
					ID:       "1",
					Priority: 2,
					Filters: map[string][]string{
						"test_group":  {"group_0"},
						"audio_model": {"whisper"},
					},
				},
				{
					ID:       "2",
					Priority: 1,
					Filters: map[string][]string{
						"test_group":  {"group_1"},
						"audio_model": {"deepgram"},
					},
				},
			},
			wantCTEs: []string{
				"filter_matches AS",
				"matched_events AS",
				"best_matches AS",
			},
			wantArgs: []interface{}{
				"1", "test_group", "group_0", "audio_model", "whisper",
				"2", "test_group", "group_1", "audio_model", "deepgram",
			},
		},
		{
			name: "single filter group",
			meterConfig: &meter.Meter{
				EventName: "audio_transcription",
				Filters: []meter.Filter{
					{Key: "test_group", Values: []string{"group_0"}},
				},
			},
			groups: []events.FilterGroup{
				{
					ID:       "1",
					Priority: 1,
					Filters: map[string][]string{
						"test_group": {"group_0"},
					},
				},
			},
			wantCTEs: []string{
				"filter_matches AS",
				"matched_events AS",
				"best_matches AS",
			},
			wantArgs: []interface{}{"1", "test_group", "group_0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			qb.WithFilterGroups(ctx, tt.groups)
			sql, args := qb.Build()

			// Verify all CTEs are present
			for _, cte := range tt.wantCTEs {
				assert.Contains(t, sql, cte)
			}

			// Verify the filter conditions are parameterized, not inlined literals
			assert.Contains(t, sql, "JSONExtractString(properties, ?) = ?")
			assert.NotContains(t, sql, "'group_0'")
			assert.NotContains(t, sql, "'test_group'")

			// Verify all group IDs, filter keys, and values were bound as args
			// (map iteration order is non-deterministic within a group, so compare as sets).
			assert.ElementsMatch(t, tt.wantArgs, args)
		})
	}
}

func TestQueryBuilder_WithAggregation(t *testing.T) {
	tests := []struct {
		name         string
		aggType      types.AggregationType
		propertyName string
		wantSQL      string
	}{
		{
			name:    "count aggregation",
			aggType: types.AggregationCount,
			wantSQL: "SELECT best_match_group as filter_group_id, COUNT(*) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
		{
			name:         "sum aggregation",
			aggType:      types.AggregationSum,
			propertyName: "duration",
			wantSQL:      "SELECT best_match_group as filter_group_id, SUM(CAST(JSONExtractString(properties, ?) AS Float64)) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
		{
			name:         "avg aggregation",
			aggType:      types.AggregationAvg,
			propertyName: "response_time",
			wantSQL:      "SELECT best_match_group as filter_group_id, AVG(CAST(JSONExtractString(properties, ?) AS Float64)) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
		{
			name:         "count unique aggregation",
			aggType:      types.AggregationCountUnique,
			propertyName: "region",
			wantSQL:      "SELECT best_match_group as filter_group_id, COUNT(DISTINCT JSONExtractString(properties, ?)) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
		{
			name:         "count unique aggregation with user property",
			aggType:      types.AggregationCountUnique,
			propertyName: "user",
			wantSQL:      "SELECT best_match_group as filter_group_id, COUNT(DISTINCT JSONExtractString(properties, ?)) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, &events.UsageParams{EventName: "test"})
			qb.WithFilterGroups(ctx, []events.FilterGroup{{ID: "1"}})
			qb.WithAggregation(ctx, tt.aggType, tt.propertyName)
			sql, args := qb.Build()
			assert.Contains(t, sql, tt.wantSQL)
			if tt.propertyName != "" {
				assert.Contains(t, args, tt.propertyName)
			}
		})
	}
}

func TestQueryBuilder_CompleteFlow(t *testing.T) {
	tests := []struct {
		name    string
		params  *events.UsageParams
		groups  []events.FilterGroup
		aggType types.AggregationType
		wantSQL string
	}{
		{
			name: "complete flow with multiple filter groups and sum aggregation",
			params: &events.UsageParams{
				EventName:          "audio_transcription",
				StartTime:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndTime:            time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
				CustomerID:         "cust_123",
				ExternalCustomerID: "ext_123",
			},
			groups: []events.FilterGroup{
				{
					ID:       "1",
					Priority: 2,
					Filters: map[string][]string{
						"test_group":  {"group_0"},
						"audio_model": {"whisper"},
					},
				},
				{
					ID:       "2",
					Priority: 1,
					Filters: map[string][]string{
						"test_group":  {"group_1"},
						"audio_model": {"deepgram"},
					},
				},
			},
			aggType: types.AggregationSum,
			wantSQL: "SELECT best_match_group as filter_group_id, SUM(CAST(JSONExtractString(properties, ?) AS Float64)) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, tt.params)
			qb.WithFilterGroups(ctx, tt.groups)
			qb.WithAggregation(ctx, tt.aggType, "duration")
			sql, args := qb.Build()
			assert.Contains(t, sql, tt.wantSQL)
			assert.Contains(t, args, "duration")
		})
	}
}
