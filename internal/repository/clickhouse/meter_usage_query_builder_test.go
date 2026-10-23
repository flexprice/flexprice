package clickhouse

import (
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/require"
)

// TestBuildDetailedGroupByColumns_ExternalCustomerID is a focused test for
// the Change-2 additive extension: "external_customer_id" is a real, indexed
// column on meter_usage, so it must be selected/grouped directly (no
// JSONExtract), unlike "properties.<field>".
func TestBuildDetailedGroupByColumns_ExternalCustomerID(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	result, err := qb.BuildDetailedGroupByColumns(&events.MeterUsageDetailedAnalyticsParams{
		GroupBy: []string{"external_customer_id"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"external_customer_id"}, result.Columns)
	require.Equal(t, []string{"external_customer_id"}, result.Aliases)
	require.Equal(t, "external_customer_id", result.FieldMapping["external_customer_id"])

	for _, col := range result.Columns {
		require.NotContains(t, col, "JSONExtractString", "external_customer_id is a real column, not a JSON property")
	}
}

// TestBuildDetailedGroupByColumns_ExternalCustomerIDAlongsideExisting proves
// the new entry composes with the pre-existing allowlist (meter_id, source,
// properties.*) without disturbing their output — purely additive.
func TestBuildDetailedGroupByColumns_ExternalCustomerIDAlongsideExisting(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	result, err := qb.BuildDetailedGroupByColumns(&events.MeterUsageDetailedAnalyticsParams{
		GroupBy: []string{"meter_id", "source", "external_customer_id", "properties.region"},
	})
	require.NoError(t, err)
	require.Equal(t, []string{"meter_id", "source", "external_customer_id", "JSONExtractString(properties, 'region')"}, result.Columns)
	require.Equal(t, "meter_id", result.FieldMapping["meter_id"])
	require.Equal(t, "source", result.FieldMapping["source"])
	require.Equal(t, "external_customer_id", result.FieldMapping["external_customer_id"])
	require.Equal(t, "prop_region", result.FieldMapping["properties.region"])
}

// TestBuildDetailedPointsQuery_GroupsByAggregateColumns proves one points query
// returns every group: it selects and groups by the aggregate's group-by columns plus window_start.
func TestBuildDetailedPointsQuery_GroupsByAggregateColumns(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		WindowSize:    types.WindowSizeWeek,
		GroupBy:       []string{"meter_id", "source", "external_customer_id", "properties.user_id"},
	}
	groupByResult, err := qb.BuildDetailedGroupByColumns(params)
	require.NoError(t, err)

	query, _ := qb.BuildDetailedPointsQuery(params, groupByResult)
	require.Contains(t, query, "JSONExtractString(properties, 'user_id') AS prop_user_id")
	require.Contains(t, query, "GROUP BY meter_id, source, external_customer_id, JSONExtractString(properties, 'user_id'), window_start")
	require.Less(t, strings.Index(query, "prop_user_id"), strings.Index(query, "AS window_start"),
		"group columns must precede window_start so the scanner reads them first")
}

// TestBuildDetailedPointsQuery_NoPerGroupPredicates proves the points query binds only
// the base filters, so a group with an empty value is grouped rather than filtered.
func TestBuildDetailedPointsQuery_NoPerGroupPredicates(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		WindowSize:    types.WindowSizeDay,
		GroupBy:       []string{"meter_id", "external_customer_id", "properties.user_id"},
	}
	groupByResult, err := qb.BuildDetailedGroupByColumns(params)
	require.NoError(t, err)

	query, args := qb.BuildDetailedPointsQuery(params, groupByResult)
	_, baseArgs := qb.BuildDetailedWhereClause(params)
	require.Equal(t, baseArgs, args)
	require.NotContains(t, query, "meter_id = ?")
	require.NotContains(t, query, "external_customer_id = ?")
	require.NotContains(t, query, "JSONExtractString(properties, ?) = ?")
}

func TestBuildDetailedGroupByColumns_RejectsUnknownEntry(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	_, err := qb.BuildDetailedGroupByColumns(&events.MeterUsageDetailedAnalyticsParams{
		GroupBy: []string{"not_a_real_column"},
	})
	require.Error(t, err)
}
