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
		wantArgs map[string]interface{}
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
			wantSQL: "WITH base_events AS (SELECT * FROM (SELECT DISTINCT ON (id, tenant_id, external_customer_id, customer_id, event_name) * FROM events WHERE event_name = 'audio_transcription' AND tenant_id = '00000000-0000-0000-0000-000000000000' AND timestamp >= toDateTime64('2024-01-01 00:00:00.000', 3) AND timestamp < toDateTime64('2024-01-02 00:00:00.000', 3) AND external_customer_id = 'ext_123' AND customer_id = 'cust_123' ORDER BY id, tenant_id, external_customer_id, customer_id, event_name, timestamp DESC))",
		},
		{
			name: "base filters without customer ID",
			params: &events.UsageParams{
				EventName: "api_calls",
				StartTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndTime:   time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
			},
			wantSQL: "WITH base_events AS (SELECT * FROM (SELECT DISTINCT ON (id, tenant_id, external_customer_id, customer_id, event_name) * FROM events WHERE event_name = 'api_calls' AND tenant_id = '00000000-0000-0000-0000-000000000000' AND timestamp >= toDateTime64('2024-01-01 00:00:00.000', 3) AND timestamp < toDateTime64('2024-01-02 00:00:00.000', 3) ORDER BY id, tenant_id, external_customer_id, customer_id, event_name, timestamp DESC))",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, tt.params)
			sql, _ := qb.Build()
			sql = strings.ReplaceAll(sql, "\n", "")
			sql = strings.ReplaceAll(sql, "\t", "")
			expected := strings.ReplaceAll(tt.wantSQL, "\n", "")
			expected = strings.ReplaceAll(expected, "\t", "")
			assert.Equal(t, expected, sql)
		})
	}
}

func TestQueryBuilder_WithFilterGroups(t *testing.T) {
	tests := []struct {
		name        string
		meterConfig *meter.Meter
		groups      []events.FilterGroup
		wantCTEs    []string
		wantFilters []string
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
			wantFilters: []string{
				"JSONExtractString(properties, 'test_group') = 'group_0'",
				"JSONExtractString(properties, 'audio_model') = 'whisper'",
				"JSONExtractString(properties, 'test_group') = 'group_1'",
				"JSONExtractString(properties, 'audio_model') = 'deepgram'",
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
			wantFilters: []string{
				"JSONExtractString(properties, 'test_group') = 'group_0'",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			qb.WithFilterGroups(ctx, tt.groups)
			sql, _ := qb.Build()

			// Verify all CTEs are present
			for _, cte := range tt.wantCTEs {
				assert.Contains(t, sql, cte)
			}

			// Verify all filters are present
			for _, filter := range tt.wantFilters {
				assert.Contains(t, sql, filter)
			}
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
			wantSQL:      "SELECT best_match_group as filter_group_id, SUM(CAST(JSONExtractString(properties, 'duration') AS Float64)) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
		{
			name:         "avg aggregation",
			aggType:      types.AggregationAvg,
			propertyName: "response_time",
			wantSQL:      "SELECT best_match_group as filter_group_id, AVG(CAST(JSONExtractString(properties, 'response_time') AS Float64)) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
		{
			name:         "count unique aggregation",
			aggType:      types.AggregationCountUnique,
			propertyName: "region",
			wantSQL:      "SELECT best_match_group as filter_group_id, COUNT(DISTINCT JSONExtractString(properties, 'region')) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
		{
			name:         "count unique aggregation with user property",
			aggType:      types.AggregationCountUnique,
			propertyName: "user",
			wantSQL:      "SELECT best_match_group as filter_group_id, COUNT(DISTINCT JSONExtractString(properties, 'user')) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, &events.UsageParams{EventName: "test"})
			qb.WithFilterGroups(ctx, []events.FilterGroup{{ID: "1"}})
			qb.WithAggregation(ctx, tt.aggType, tt.propertyName)
			sql, _ := qb.Build()
			assert.Contains(t, sql, tt.wantSQL)
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
			wantSQL: "SELECT best_match_group as filter_group_id, SUM(CAST(JSONExtractString(properties, 'duration') AS Float64)) as value FROM best_matches GROUP BY best_match_group ORDER BY best_match_group",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, tt.params)
			qb.WithFilterGroups(ctx, tt.groups)
			qb.WithAggregation(ctx, tt.aggType, "duration")
			sql, _ := qb.Build()
			assert.Contains(t, sql, tt.wantSQL)
		})
	}
}
