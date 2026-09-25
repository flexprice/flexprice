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

// TestBuildDetailedPointsQuery_NarrowsByExternalCustomerID proves the
// per-group points sub-query added by Change 2 narrows to the same customer
// as the aggregate row it belongs to, mirroring the existing Source narrowing.
func TestBuildDetailedPointsQuery_NarrowsByExternalCustomerID(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		WindowSize:    types.WindowSizeDay,
	}
	groupByResult, err := qb.BuildDetailedGroupByColumns(&events.MeterUsageDetailedAnalyticsParams{
		GroupBy: []string{"external_customer_id"},
	})
	require.NoError(t, err)

	result := &events.MeterUsageDetailedResult{ExternalCustomerID: "cust_1"}
	query, args := qb.BuildDetailedPointsQuery(params, result, groupByResult)
	require.Contains(t, query, "external_customer_id = ?")
	require.Contains(t, args, "cust_1")
	require.Equal(t, 1, strings.Count(query, "external_customer_id = ?"))
}

// TestBuildDetailedPointsQuery_ConstrainsEmptyCustomerGroup proves the points
// sub-query still constrains external_customer_id for the empty-ID group (events
// with no customer). Without the group predicate the sub-query would sum every
// customer allowed by the base filters, disagreeing with the aggregate row that
// only holds the empty-ID group.
func TestBuildDetailedPointsQuery_ConstrainsEmptyCustomerGroup(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		WindowSize:    types.WindowSizeDay,
	}
	groupByResult, err := qb.BuildDetailedGroupByColumns(&events.MeterUsageDetailedAnalyticsParams{
		GroupBy: []string{"external_customer_id"},
	})
	require.NoError(t, err)

	result := &events.MeterUsageDetailedResult{ExternalCustomerID: ""} // empty-ID group
	query, args := qb.BuildDetailedPointsQuery(params, result, groupByResult)
	require.Contains(t, query, "external_customer_id = ?")
	require.Equal(t, 1, strings.Count(query, "external_customer_id = ?"))
	// The empty group value must be bound as the last arg, not dropped.
	require.Equal(t, "", args[len(args)-1])
}

// TestBuildDetailedPointsQuery_NoStructuralPredicateWhenNotGrouped proves a
// dimension that was not grouped adds no per-group predicate, even if the
// result carries a stray value — the points query relies on the base filters.
func TestBuildDetailedPointsQuery_NoStructuralPredicateWhenNotGrouped(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		WindowSize:    types.WindowSizeDay,
	}
	groupByResult, err := qb.BuildDetailedGroupByColumns(&events.MeterUsageDetailedAnalyticsParams{
		GroupBy: []string{"meter_id"},
	})
	require.NoError(t, err)

	result := &events.MeterUsageDetailedResult{MeterID: "meter_1", ExternalCustomerID: "leftover"}
	query, _ := qb.BuildDetailedPointsQuery(params, result, groupByResult)
	require.Contains(t, query, "meter_id = ?")
	require.NotContains(t, query, "external_customer_id = ?")
}

func TestBuildDetailedPointsQuery_ConstrainsEmptyPropertyGroup(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		WindowSize:    types.WindowSizeWeek,
		GroupBy:       []string{"source", "meter_id", "properties.user_id"},
	}
	groupByResult, err := qb.BuildDetailedGroupByColumns(params)
	require.NoError(t, err)

	// Untagged row: scanner dropped empty user_id from Properties.
	result := &events.MeterUsageDetailedResult{
		MeterID:    "meter_1",
		Source:     "src_1",
		Properties: map[string]string{},
	}
	query, args := qb.BuildDetailedPointsQuery(params, result, groupByResult)
	require.Contains(t, query, "JSONExtractString(properties, ?) = ?")
	require.Contains(t, args, "user_id")
	require.Contains(t, args, "")
	require.Contains(t, query, "source = ?")
	require.Contains(t, query, "meter_id = ?")
}

func TestBuildDetailedPointsQuery_NarrowsByNonEmptyProperty(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	params := &events.MeterUsageDetailedAnalyticsParams{
		TenantID:      "tenant_1",
		EnvironmentID: "env_1",
		WindowSize:    types.WindowSizeWeek,
		GroupBy:       []string{"properties.user_id"},
	}
	groupByResult, err := qb.BuildDetailedGroupByColumns(params)
	require.NoError(t, err)

	result := &events.MeterUsageDetailedResult{
		Properties: map[string]string{"user_id": "abc"},
	}
	query, args := qb.BuildDetailedPointsQuery(params, result, groupByResult)
	require.Contains(t, query, "JSONExtractString(properties, ?) = ?")
	require.Equal(t, 1, strings.Count(query, "JSONExtractString(properties, ?) = ?"))
	require.Equal(t, "user_id", args[len(args)-2])
	require.Equal(t, "abc", args[len(args)-1])
}

func TestBuildDetailedGroupByColumns_RejectsUnknownEntry(t *testing.T) {
	qb := NewMeterUsageQueryBuilder()

	_, err := qb.BuildDetailedGroupByColumns(&events.MeterUsageDetailedAnalyticsParams{
		GroupBy: []string{"not_a_real_column"},
	})
	require.Error(t, err)
}
