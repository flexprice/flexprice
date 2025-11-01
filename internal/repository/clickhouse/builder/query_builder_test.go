package builder

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test helper functions
func createTestContext(tenantID, environmentID string) context.Context {
	ctx := context.Background()
	if tenantID != "" {
		ctx = types.SetTenantID(ctx, tenantID)
	}
	if environmentID != "" {
		ctx = types.SetEnvironmentID(ctx, environmentID)
	}
	return ctx
}

func createTestUsageParams(eventName string, startTime, endTime time.Time) *events.UsageParams {
	return &events.UsageParams{
		EventName:          eventName,
		PropertyName:       "test_property",
		AggregationType:    types.AggregationCount,
		StartTime:          startTime,
		EndTime:            endTime,
		ExternalCustomerID: "test_customer",
		CustomerID:         "cust_123",
		Filters: map[string][]string{
			"region": {"us-west-1", "us-east-1"},
		},
	}
}

func indexOfArg(args []interface{}, target interface{}) int {
	for i, arg := range args {
		if reflect.DeepEqual(arg, target) {
			return i
		}
	}
	return -1
}

// SQL Injection Test Vectors
var sqlInjectionVectors = []string{
	"'; DROP TABLE events; --",
	"' OR 1=1; --",
	"' UNION SELECT * FROM events; --",
	"'; INSERT INTO events VALUES ('malicious'); --",
	"' OR 'x'='x",
	"\"; DROP TABLE events; --",
	"' AND SLEEP(5); --",
	"'; EXEC xp_cmdshell('dir'); --",
	"1' OR '1'='1",
	"admin'--",
	"admin'/*",
	"' OR 1=1#",
	"' OR 'a'='a",
	"') OR ('1'='1",
	"'; DELETE FROM events WHERE 1=1; --",
	"' OR EXISTS(SELECT * FROM events); --",
	"' UNION ALL SELECT NULL,NULL,NULL; --",
}

// TestSQLInjectionPreventionEventName tests SQL injection prevention for event name parameter
func TestSQLInjectionPreventionEventName(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")

	for i, maliciousInput := range sqlInjectionVectors {
		t.Run(fmt.Sprintf("EventName_Vector_%d", i), func(t *testing.T) {
			startTime := time.Now().Add(-24 * time.Hour)
			endTime := time.Now()

			params := createTestUsageParams(maliciousInput, startTime, endTime)

			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, params)

			query, args := qb.Build()

			// Verify that the malicious input is parameterized, not injected directly
			assert.NotContains(t, query, maliciousInput, "Malicious input should not appear directly in query")

			// Verify the malicious input is in the args (properly parameterized)
			assert.Contains(t, args, maliciousInput, "Malicious input should be in parameterized args")

			// Verify query uses proper placeholders
			assert.Contains(t, query, "event_name = ?", "Query should use parameterized placeholders")

			// Verify no SQL keywords from injection attempt appear unescaped
			for _, keyword := range []string{"DROP", "UNION", "INSERT", "DELETE", "EXEC"} {
				if strings.Contains(maliciousInput, keyword) {
					assert.NotContains(t, query, keyword, "SQL keywords should not appear unescaped in query")
				}
			}
		})
	}
}

// TestSQLInjectionPreventionCustomerIDs tests SQL injection prevention for customer ID parameters
func TestSQLInjectionPreventionCustomerIDs(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	testCases := []struct {
		name      string
		setupFunc func(*events.UsageParams, string)
	}{
		{
			name: "ExternalCustomerID",
			setupFunc: func(params *events.UsageParams, malicious string) {
				params.ExternalCustomerID = malicious
			},
		},
		{
			name: "CustomerID",
			setupFunc: func(params *events.UsageParams, malicious string) {
				params.CustomerID = malicious
			},
		},
	}

	for _, tc := range testCases {
		for i, maliciousInput := range sqlInjectionVectors {
			t.Run(fmt.Sprintf("%s_Vector_%d", tc.name, i), func(t *testing.T) {
				params := createTestUsageParams("test_event", startTime, endTime)
				tc.setupFunc(params, maliciousInput)

				qb := NewQueryBuilder()
				qb.WithBaseFilters(ctx, params)

				query, args := qb.Build()

				// Verify that the malicious input is parameterized
				assert.NotContains(t, query, maliciousInput, "Malicious input should not appear directly in query")
				assert.Contains(t, args, maliciousInput, "Malicious input should be in parameterized args")
			})
		}
	}
}

// TestSQLInjectionPreventionFilterValues tests SQL injection prevention for filter values
func TestSQLInjectionPreventionFilterValues(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	for i, maliciousInput := range sqlInjectionVectors {
		t.Run(fmt.Sprintf("FilterValue_Vector_%d", i), func(t *testing.T) {
			params := createTestUsageParams("test_event", startTime, endTime)
			params.Filters = map[string][]string{
				"test_property": {maliciousInput},
			}

			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, params)

			query, args := qb.Build()

			// Verify malicious input is parameterized
			assert.NotContains(t, query, maliciousInput, "Malicious input should not appear directly in query")
			assert.Contains(t, args, maliciousInput, "Malicious input should be in parameterized args")
		})
	}
}

// TestBaseFilterArgumentOrdering ensures property arguments precede their values for multi-value filters
func TestBaseFilterArgumentOrdering(t *testing.T) {
	ctx := context.Background()
	params := &events.UsageParams{
		EventName: "test_event",
		Filters: map[string][]string{
			"region": {"us-west", "us-east"},
		},
	}

	qb := NewQueryBuilder()
	qb.WithBaseFilters(ctx, params)
	query, args := qb.Build()

	regionIdx := indexOfArg(args, "region")
	westIdx := indexOfArg(args, "us-west")
	eastIdx := indexOfArg(args, "us-east")

	require.NotEqual(t, -1, regionIdx, "region property should be in args")
	require.NotEqual(t, -1, westIdx, "us-west value should be in args")
	require.NotEqual(t, -1, eastIdx, "us-east value should be in args")

	assert.Less(t, regionIdx, westIdx, "property argument must precede its values to align numbered placeholders")
	assert.Less(t, westIdx, eastIdx, "values should maintain insertion order")
	assert.NotContains(t, query, "'region'", "property names should stay parameterized")
}

// TestSQLInjectionPreventionFilterGroups tests SQL injection prevention for filter group values
func TestSQLInjectionPreventionFilterGroups(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	for i, maliciousInput := range sqlInjectionVectors {
		t.Run(fmt.Sprintf("FilterGroup_Vector_%d", i), func(t *testing.T) {
			params := createTestUsageParams("test_event", startTime, endTime)

			filterGroups := []events.FilterGroup{
				{
					ID:       "group1",
					Priority: 1,
					Filters: map[string][]string{
						"malicious_property": {maliciousInput},
					},
				},
			}

			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, params)
			qb.WithFilterGroups(ctx, filterGroups)

			query, args := qb.Build()

			// Verify malicious input is parameterized
			assert.NotContains(t, query, maliciousInput, "Malicious input should not appear directly in query")
			assert.Contains(t, args, maliciousInput, "Malicious input should be in parameterized args")

			// Verify args has expected length
			assert.Greater(t, len(args), 0, "Args should contain parameters")
		})
	}
}

// TestFilterGroupArgumentOrdering ensures grouped filters maintain property/value ordering
func TestFilterGroupArgumentOrdering(t *testing.T) {
	ctx := context.Background()
	params := &events.UsageParams{
		EventName: "test_event",
	}

	filterGroups := []events.FilterGroup{
		{
			ID:       "group1",
			Priority: 7,
			Filters: map[string][]string{
				"status": {"pending", "complete"},
			},
		},
	}

	qb := NewQueryBuilder()
	qb.WithBaseFilters(ctx, params)
	qb.WithFilterGroups(ctx, filterGroups)
	qb.WithAggregation(ctx, types.AggregationCount, "")

	query, args := qb.Build()

	groupIDIdx := indexOfArg(args, "group1")
	priorityIdx := indexOfArg(args, filterGroups[0].Priority)
	statusIdx := indexOfArg(args, "status")
	pendingIdx := indexOfArg(args, "pending")
	completeIdx := indexOfArg(args, "complete")

	require.NotEqual(t, -1, groupIDIdx, "group id should be parameterized")
	require.NotEqual(t, -1, priorityIdx, "group priority should be parameterized")
	require.NotEqual(t, -1, statusIdx, "status property should be parameterized")
	require.NotEqual(t, -1, pendingIdx, "pending value should be parameterized")
	require.NotEqual(t, -1, completeIdx, "complete value should be parameterized")

	assert.Less(t, statusIdx, pendingIdx, "group property argument must precede first value")
	assert.Less(t, pendingIdx, completeIdx, "group values should maintain insertion order")
	assert.NotContains(t, query, "'status'", "group property must not leak into SQL text")
}

// TestSQLInjectionPreventionPropertyNames tests SQL injection prevention for property names
func TestSQLInjectionPreventionPropertyNames(t *testing.T) {
	maliciousPropertyNames := []string{
		"'; DROP TABLE events; --",
		"property') OR 1=1; --",
		"prop'; DELETE FROM events; --",
	}

	for i, maliciousProp := range maliciousPropertyNames {
		t.Run(fmt.Sprintf("PropertyName_Vector_%d", i), func(t *testing.T) {
			qb := NewQueryBuilder()
			ctx := context.Background()
			params := &events.UsageParams{
				EventName:          "test_event",
				ExternalCustomerID: "cust_123",
				CustomerID:         "customer_456",
				StartTime:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				EndTime:            time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
				Filters: map[string][]string{
					maliciousProp: {"value1"},
				},
			}

			qb.WithBaseFilters(ctx, params)
			query, args := qb.Build()

			// Verify that malicious property names are NOT directly in the query
			assert.NotContains(t, query, maliciousProp, "Malicious property name should be parameterized, not in query")

			// Verify the property name is passed as a parameter instead
			found := false
			for _, arg := range args {
				if str, ok := arg.(string); ok && str == maliciousProp {
					found = true
					break
				}
			}
			assert.True(t, found, "Property name should be passed as parameter")

			// Verify parameterized pattern exists
			assert.Contains(t, query, "JSONExtractString(properties, ?", "Should use parameterized property extraction")
		})
	}
}

// TestParameterizedQueryValidation tests that all user inputs are properly parameterized
func TestParameterizedQueryValidation(t *testing.T) {
	qb := NewQueryBuilder()
	ctx := context.Background()
	params := &events.UsageParams{
		EventName:          "test_event",
		ExternalCustomerID: "cust_123",
		CustomerID:         "customer_456",
		StartTime:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:            time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		Filters: map[string][]string{
			"region": {"us-west", "us-east"},
			"tier":   {"premium"},
		},
	}

	qb.WithBaseFilters(ctx, params)
	qb.WithFilterGroups(ctx, []events.FilterGroup{
		{
			ID:       "group1",
			Priority: 1,
			Filters:  map[string][]string{"category": {"premium"}},
		},
	})
	qb.WithAggregation(ctx, types.AggregationSum, "amount")

	query, args := qb.Build()

	// Verify the query captures all critical user inputs through parameters rather than string interpolation
	expectedArgs := []interface{}{
		params.EventName,
		params.ExternalCustomerID,
		params.CustomerID,
		"region",
		"us-west",
		"us-east",
		"tier",
		"premium",
		"group1",
		1,
		"category",
		"amount",
	}

	for _, expected := range expectedArgs {
		idx := indexOfArg(args, expected)
		require.NotEqualf(t, -1, idx, "expected argument %v to be parameterized", expected)
	}

	for _, expected := range []time.Time{params.StartTime, params.EndTime} {
		idx := indexOfArg(args, expected)
		require.NotEqualf(t, -1, idx, "expected time argument %v to be parameterized", expected)
	}

	assert.GreaterOrEqual(t, len(args), len(expectedArgs)+2, "should at least contain the expected arguments and time bounds")

	// Verify category filter is parameterized (not hardcoded)
	categoryPattern := regexp.MustCompile(`JSONExtractString\(properties, \?\d+\) = \?\d+`)
	assert.Regexp(t, categoryPattern, query, "Category filter should be parameterized")

	// Verify no hardcoded property names in the final query
	assert.NotContains(t, query, "'category'", "Property names should be parameterized, not hardcoded")
	assert.NotContains(t, query, "'region'", "Property names should be parameterized, not hardcoded")
	assert.NotContains(t, query, "'tier'", "Property names should be parameterized, not hardcoded")
}

// TestTimeConditionsParameterization tests that time values are properly parameterized as time.Time
func TestTimeConditionsParameterization(t *testing.T) {
	tests := []struct {
		name       string
		startTime  time.Time
		endTime    time.Time
		expectArgs bool
	}{
		{
			name:       "Normal time range",
			startTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			endTime:    time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expectArgs: true,
		},
		{
			name:       "Zero start time",
			startTime:  time.Time{},
			endTime:    time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			expectArgs: true,
		},
		{
			name:       "Zero end time",
			startTime:  time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			endTime:    time.Time{},
			expectArgs: true,
		},
		{
			name:       "Far future time",
			startTime:  time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC),
			endTime:    time.Date(2031, 1, 1, 0, 0, 0, 0, time.UTC),
			expectArgs: true,
		},
		{
			name:       "Very old time",
			startTime:  time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
			endTime:    time.Date(2020, 1, 31, 23, 59, 59, 0, time.UTC),
			expectArgs: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qb := NewQueryBuilder()
			ctx := context.Background()
			params := &events.UsageParams{
				EventName: "test_event",
				StartTime: tt.startTime,
				EndTime:   tt.endTime,
			}

			qb.WithBaseFilters(ctx, params)
			query, args := qb.Build()

			if tt.expectArgs {
				// Check that time values are passed as time.Time objects
				hasTimeArg := false
				for _, arg := range args {
					if _, ok := arg.(time.Time); ok {
						hasTimeArg = true
						break
					}
				}
				assert.True(t, hasTimeArg, "Should have time.Time object in args")
			}

			// Verify query structure is correct (expect numbered parameters)
			if !tt.startTime.IsZero() {
				assert.Contains(t, query, "timestamp >= toDateTime64(?", "Should have parameterized start time")
			}
			if !tt.endTime.IsZero() {
				assert.Contains(t, query, "timestamp < toDateTime64(?", "Should have parameterized end time")
			}
		})
	}
}

// TestFunctionalEquivalenceWithBaseFilters tests functional equivalence of WithBaseFilters
func TestFunctionalEquivalenceWithBaseFilters(t *testing.T) {
	qb := NewQueryBuilder()
	ctx := context.Background()
	params := &events.UsageParams{
		EventName:          "images_processed",
		ExternalCustomerID: "cust_123",
		CustomerID:         "customer_456",
		StartTime:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:            time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		Filters: map[string][]string{
			"region": {"us-west", "us-east"},
			"tier":   {"premium"},
		},
	}

	qb.WithBaseFilters(ctx, params)
	query, args := qb.Build()

	// Verify basic structure
	assert.Contains(t, query, "WITH base_events AS (", "Should have base_events CTE")
	assert.Contains(t, query, "event_name = ?", "Should have parameterized event name")
	assert.Contains(t, query, "timestamp >= toDateTime64(?", "Should have parameterized start time")
	assert.Contains(t, query, "timestamp < toDateTime64(?", "Should have parameterized end time")

	// Verify time arguments are time.Time objects
	timeArgsFound := 0
	for _, arg := range args {
		if _, ok := arg.(time.Time); ok {
			timeArgsFound++
		}
	}
	assert.Equal(t, 2, timeArgsFound, "Should have 2 time.Time objects in args")

	// Verify filter property names are parameterized (not hardcoded)
	assert.NotContains(t, query, "'region'", "Region property name should be parameterized")
	assert.NotContains(t, query, "'tier'", "Tier property name should be parameterized")

	// Verify property names are in arguments
	hasRegionProp := false
	hasTierProp := false
	for _, arg := range args {
		if str, ok := arg.(string); ok {
			if str == "region" {
				hasRegionProp = true
			}
			if str == "tier" {
				hasTierProp = true
			}
		}
	}
	assert.True(t, hasRegionProp, "Region property should be in arguments")
	assert.True(t, hasTierProp, "Tier property should be in arguments")
}

// TestFunctionalEquivalenceWithFilterGroups tests functional equivalence of WithFilterGroups
func TestFunctionalEquivalenceWithFilterGroups(t *testing.T) {
	qb := NewQueryBuilder()
	ctx := context.Background()
	params := &events.UsageParams{
		EventName: "images_processed",
		Filters: map[string][]string{
			"region": {"us-west", "us-east"},
		},
	}

	qb.WithBaseFilters(ctx, params)
	qb.WithFilterGroups(ctx, []events.FilterGroup{
		{
			ID:       "tier_premium_group",
			Priority: 3,
			Filters:  map[string][]string{"tier": {"premium"}},
		},
		{
			ID:       "tier_standard_group",
			Priority: 2,
			Filters:  map[string][]string{"tier": {"standard"}},
		},
		{
			ID:       "tier_basic_group",
			Priority: 1,
			Filters:  map[string][]string{"tier": {"basic"}},
		},
	})
	qb.WithAggregation(ctx, types.AggregationCount, "")

	query, _ := qb.Build()

	// Verify the query contains basic structure (more flexible checks)
	assert.Contains(t, query, "filter_matches AS (", "Should have filter_matches CTE")
	assert.Contains(t, query, "arrayMap", "Should have arrayMap for filter matches")
	assert.Contains(t, query, "group_matches", "Should have group_matches field")

	// Verify parameterized patterns
	assert.Contains(t, query, "JSONExtractString(properties, ?", "Should use parameterized property extraction")
}

// TestFunctionalEquivalenceWithAggregation tests functional equivalence of WithAggregation
func TestFunctionalEquivalenceWithAggregation(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")

	testCases := []struct {
		name         string
		aggType      types.AggregationType
		propertyName string
		expectedSQL  string
	}{
		{
			name:         "COUNT aggregation",
			aggType:      types.AggregationCount,
			propertyName: "",
			expectedSQL:  "COUNT(*)",
		},
		{
			name:         "SUM aggregation",
			aggType:      types.AggregationSum,
			propertyName: "amount",
			expectedSQL:  "SUM(CAST(JSONExtractString(properties, ?",
		},
		{
			name:         "AVG aggregation",
			aggType:      types.AggregationAvg,
			propertyName: "duration",
			expectedSQL:  "AVG(CAST(JSONExtractString(properties, ?",
		},
		{
			name:         "COUNT_UNIQUE aggregation",
			aggType:      types.AggregationCountUnique,
			propertyName: "user_id",
			expectedSQL:  "COUNT(DISTINCT JSONExtractString(properties, ?",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			startTime := time.Now().Add(-24 * time.Hour)
			endTime := time.Now()
			params := createTestUsageParams("test_event", startTime, endTime)

			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, params)
			qb.WithAggregation(ctx, tc.aggType, tc.propertyName)

			query, args := qb.Build()

			// Verify aggregation clause is present
			assert.Contains(t, query, tc.expectedSQL)

			// Verify property name is parameterized (if required)
			if tc.propertyName != "" {
				assert.Contains(t, args, tc.propertyName)
				assert.NotContains(t, strings.ReplaceAll(query, "?", "PARAM"), tc.propertyName, "Property name should not appear raw in aggregation")
			}

			// Verify final query structure
			assert.Contains(t, query, "SELECT best_match_group as filter_group_id")
			assert.Contains(t, query, "as value FROM best_matches")
			assert.Contains(t, query, "GROUP BY best_match_group")
			assert.Contains(t, query, "ORDER BY best_match_group")
		})
	}
}

// TestComplexQueryBuilding tests complex query building scenarios
func TestComplexQueryBuilding(t *testing.T) {
	ctx := createTestContext("tenant_complex", "env_prod")
	startTime := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 6, 30, 23, 59, 59, 0, time.UTC)

	params := &events.UsageParams{
		EventName:          "image_processing",
		PropertyName:       "processing_time",
		AggregationType:    types.AggregationSum,
		StartTime:          startTime,
		EndTime:            endTime,
		ExternalCustomerID: "customer_premium_001",
		CustomerID:         "cust_001",
		Filters: map[string][]string{
			"image_format": {"jpeg", "png", "webp"},
			"quality":      {"high"},
			"region":       {"us-east-1", "us-west-2", "eu-west-1"},
		},
	}

	filterGroups := []events.FilterGroup{
		{
			ID:       "large_images",
			Priority: 5,
			Filters: map[string][]string{
				"size_category": {"xl", "xxl"},
				"resolution":    {"4k", "8k"},
			},
		},
		{
			ID:       "medium_images",
			Priority: 3,
			Filters: map[string][]string{
				"size_category": {"medium", "large"},
				"resolution":    {"1080p", "2k"},
			},
		},
		{
			ID:       "small_images",
			Priority: 1,
			Filters: map[string][]string{
				"size_category": {"small"},
				"resolution":    {"720p"},
			},
		},
	}

	qb := NewQueryBuilder()
	qb.WithBaseFilters(ctx, params)
	qb.WithFilterGroups(ctx, filterGroups)
	qb.WithAggregation(ctx, types.AggregationSum, "processing_time")

	query, args := qb.Build()

	// Verify complete query structure
	assert.Contains(t, query, "WITH")
	assert.Contains(t, query, "base_events AS")
	assert.Contains(t, query, "filter_matches AS")
	assert.Contains(t, query, "SELECT")

	// Verify all CTEs are properly constructed
	cteParts := []string{
		"base_events AS",
		"filter_matches AS",
	}
	for _, part := range cteParts {
		assert.Contains(t, query, part, "Query should contain CTE: "+part)
	}

	// Verify parameter count is reasonable
	expectedMinParams := 15 // Base params + filter values + aggregation
	assert.GreaterOrEqual(t, len(args), expectedMinParams, "Should have sufficient parameters")

	// Verify no SQL injection vulnerabilities
	dangerousPatterns := []string{
		"'; DROP",
		"' OR 1=1",
		"' UNION",
		"-- ",
		"/*",
		"*/",
	}
	for _, pattern := range dangerousPatterns {
		assert.NotContains(t, query, pattern, "Query should not contain dangerous pattern: "+pattern)
	}

	// Verify all user inputs are properly parameterized
	userInputs := []interface{}{
		"image_processing", "customer_premium_001", "cust_001",
		"jpeg", "png", "webp", "high",
		"us-east-1", "us-west-2", "eu-west-1",
		"xl", "xxl", "4k", "8k",
		"medium", "large", "1080p", "2k",
		"small", "720p", "processing_time",
	}

	for _, input := range userInputs {
		assert.Contains(t, args, input, "User input should be parameterized: %v", input)
	}
}

// TestEdgeCases tests edge cases and boundary conditions
func TestEdgeCases(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")

	t.Run("Empty filters", func(t *testing.T) {
		startTime := time.Now().Add(-24 * time.Hour)
		endTime := time.Now()

		params := &events.UsageParams{
			EventName:          "test_event",
			PropertyName:       "amount",
			AggregationType:    types.AggregationSum,
			StartTime:          startTime,
			EndTime:            endTime,
			ExternalCustomerID: "test_customer",
			Filters:            map[string][]string{}, // Empty filters
		}

		qb := NewQueryBuilder()
		qb.WithBaseFilters(ctx, params)

		query, args := qb.Build()

		assert.NotEmpty(t, query)
		assert.NotEmpty(t, args)
		assert.Contains(t, query, "base_events AS")

		// Verify context was used properly
		tenantID := types.GetTenantID(ctx)
		assert.Equal(t, "tenant_123", tenantID)
	})

	t.Run("Nil filters", func(t *testing.T) {
		startTime := time.Now().Add(-24 * time.Hour)
		endTime := time.Now()

		params := &events.UsageParams{
			EventName:          "test_event",
			PropertyName:       "amount",
			AggregationType:    types.AggregationSum,
			StartTime:          startTime,
			EndTime:            endTime,
			ExternalCustomerID: "test_customer",
			Filters:            nil, // Nil filters
		}

		qb := NewQueryBuilder()
		qb.WithBaseFilters(ctx, params)

		query, args := qb.Build()

		assert.NotEmpty(t, query)
		assert.NotEmpty(t, args)
	})

	t.Run("Empty filter groups", func(t *testing.T) {
		startTime := time.Now().Add(-24 * time.Hour)
		endTime := time.Now()
		params := createTestUsageParams("test_event", startTime, endTime)

		qb := NewQueryBuilder()
		qb.WithBaseFilters(ctx, params)
		qb.WithFilterGroups(ctx, []events.FilterGroup{}) // Empty filter groups

		query, args := qb.Build()

		assert.NotEmpty(t, query)
		assert.NotEmpty(t, args)
		// Should still contain base_events
		assert.Contains(t, query, "base_events AS")
	})

	t.Run("Filter_group_with_empty_filters", func(t *testing.T) {
		qb := NewQueryBuilder()
		ctx := context.Background()
		params := &events.UsageParams{
			EventName:          "test_event",
			ExternalCustomerID: "cust_123",
			CustomerID:         "customer_456",
			StartTime:          time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			EndTime:            time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
			Filters: map[string][]string{
				"region": {"us-west", "us-east"},
			},
		}

		qb.WithBaseFilters(ctx, params)
		qb.WithFilterGroups(ctx, []events.FilterGroup{
			{
				ID:       "empty_group",
				Priority: 1,
				Filters:  map[string][]string{}, // Empty filters
			},
		})

		query, args := qb.Build()

		// Since group IDs are now parameterized, we can't check for hardcoded group names
		// Instead, verify the group ID is passed as a parameter
		hasEmptyGroupID := false
		for _, arg := range args {
			if str, ok := arg.(string); ok && str == "empty_group" {
				hasEmptyGroupID = true
				break
			}
		}
		assert.True(t, hasEmptyGroupID, "Empty group ID should be passed as parameter")

		// Verify query structure handles empty filter groups
		assert.Contains(t, query, "filter_matches AS (", "Should have filter_matches CTE")
		assert.Contains(t, query, ", 1)", "Should have constant true condition for empty filter")

		// Verify the group ID is NOT hardcoded in the query (security improvement)
		assert.NotContains(t, query, "empty_group", "Group ID should be parameterized, not hardcoded")
	})

	t.Run("Very long filter values", func(t *testing.T) {
		startTime := time.Now().Add(-24 * time.Hour)
		endTime := time.Now()

		longValue := strings.Repeat("x", 1000) // 1000 character value

		params := &events.UsageParams{
			EventName:          "test_event",
			PropertyName:       "amount",
			AggregationType:    types.AggregationSum,
			StartTime:          startTime,
			EndTime:            endTime,
			ExternalCustomerID: "test_customer",
			Filters: map[string][]string{
				"long_property": {longValue},
			},
		}

		qb := NewQueryBuilder()
		qb.WithBaseFilters(ctx, params)

		query, args := qb.Build()

		// Long value should be parameterized, not in query
		assert.NotContains(t, query, longValue)
		assert.Contains(t, args, longValue)
	})
}

// TestIntegrationBuildOutput tests the final Build() method output format
func TestIntegrationBuildOutput(t *testing.T) {
	ctx := createTestContext("tenant_integration", "env_test")
	startTime := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 3, 31, 23, 59, 59, 0, time.UTC)

	params := &events.UsageParams{
		EventName:          "api_request",
		PropertyName:       "response_time",
		AggregationType:    types.AggregationAvg,
		StartTime:          startTime,
		EndTime:            endTime,
		ExternalCustomerID: "integration_customer",
		CustomerID:         "int_cust_001",
		Filters: map[string][]string{
			"status_code": {"200", "201"},
			"method":      {"GET", "POST"},
		},
	}

	filterGroups := []events.FilterGroup{
		{
			ID:       "success_group",
			Priority: 2,
			Filters: map[string][]string{
				"status_category": {"2xx"},
			},
		},
		{
			ID:       "error_group",
			Priority: 1,
			Filters: map[string][]string{
				"status_category": {"4xx", "5xx"},
			},
		},
	}

	qb := NewQueryBuilder()
	qb.WithBaseFilters(ctx, params)
	qb.WithFilterGroups(ctx, filterGroups)
	qb.WithAggregation(ctx, types.AggregationAvg, "response_time")

	query, args := qb.Build()

	// Verify query is valid SQL structure
	assert.True(t, strings.HasPrefix(query, "WITH"), "Query should start with WITH")
	assert.Contains(t, query, "SELECT", "Query should contain SELECT")
	assert.Contains(t, query, "FROM", "Query should contain FROM")
	assert.Contains(t, query, "GROUP BY", "Query should contain GROUP BY")
	assert.Contains(t, query, "ORDER BY", "Query should contain ORDER BY")

	// Verify proper CTE structure
	ctePattern := regexp.MustCompile(`WITH\s+\w+\s+AS\s+\(`)
	assert.Regexp(t, ctePattern, query, "Query should have proper CTE structure")

	// Verify argument types are correct
	for i, arg := range args {
		switch v := arg.(type) {
		case string:
			assert.NotEmpty(t, v, "String argument %d should not be empty", i)
		case time.Time:
			assert.False(t, v.IsZero(), "Time argument %d should not be zero", i)
		default:
			// Other types are acceptable
		}
	}

	// Verify parameter placeholders exist and are properly formatted
	placeholderRegex := regexp.MustCompile(`\?\d+`)
	placeholders := placeholderRegex.FindAllString(query, -1)

	// Should have placeholders present
	assert.Greater(t, len(placeholders), 0, "Query should contain parameter placeholders")

	// Verify each CTE component can have its own parameter numbering
	// (This is how the current implementation works - each component restarts numbering)
	for _, placeholder := range placeholders {
		// Just verify the format is correct: ?N where N is a positive integer
		assert.Regexp(t, `^\?\d+$`, placeholder, "Placeholder should be in correct format: %s", placeholder)
	}

	// Verify we have a reasonable number of arguments
	assert.GreaterOrEqual(t, len(args), 10, "Should have multiple arguments for complex query")
	assert.LessOrEqual(t, len(args), 50, "Should not have excessive arguments")
}

// TestTimeConditionsSecurity tests time condition security specifically
func TestTimeConditionsSecurity(t *testing.T) {
	t.Run("Boundary_time_conditions", func(t *testing.T) {
		qb := NewQueryBuilder()
		ctx := context.Background()

		// Test with boundary times
		startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		endTime := time.Date(2030, 12, 31, 23, 59, 59, 0, time.UTC)

		params := &events.UsageParams{
			EventName: "test_event",
			StartTime: startTime,
			EndTime:   endTime,
		}

		qb.WithBaseFilters(ctx, params)
		query, args := qb.Build()

		// Verify time conditions are parameterized with numbered parameters
		assert.Contains(t, query, "timestamp >= toDateTime64(?", "Should have parameterized start time")
		assert.Contains(t, query, "timestamp < toDateTime64(?", "Should have parameterized end time")

		// Verify time arguments are time.Time objects (not formatted strings)
		timeArgs := 0
		for _, arg := range args {
			if _, ok := arg.(time.Time); ok {
				timeArgs++
			}
		}
		assert.Equal(t, 2, timeArgs, "Should have 2 time.Time objects in args")

		// Verify no hardcoded time strings in query
		assert.NotContains(t, query, "2020-01-01", "Should not have hardcoded start time")
		assert.NotContains(t, query, "2030-12-31", "Should not have hardcoded end time")
	})
}

// BenchmarkQueryBuilding benchmarks query building performance
func BenchmarkQueryBuilding(b *testing.B) {
	ctx := createTestContext("tenant_bench", "env_bench")
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	params := &events.UsageParams{
		EventName:          "benchmark_event",
		PropertyName:       "value",
		AggregationType:    types.AggregationSum,
		StartTime:          startTime,
		EndTime:            endTime,
		ExternalCustomerID: "bench_customer",
		CustomerID:         "bench_cust_001",
		Filters: map[string][]string{
			"category": {"A", "B", "C"},
			"region":   {"us-east-1", "us-west-1"},
		},
	}

	filterGroups := []events.FilterGroup{
		{
			ID:       "group1",
			Priority: 1,
			Filters: map[string][]string{
				"tier": {"premium"},
			},
		},
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		qb := NewQueryBuilder()
		qb.WithBaseFilters(ctx, params)
		qb.WithFilterGroups(ctx, filterGroups)
		qb.WithAggregation(ctx, types.AggregationSum, "value")
		qb.Build()
	}
}

// Test backward compatibility
func TestBackwardCompatibility(t *testing.T) {
	// This test ensures the parameterized version maintains the same interface
	// and produces queries that are functionally equivalent to the original

	ctx := createTestContext("tenant_compat", "env_compat")
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)

	params := &events.UsageParams{
		EventName:          "compatibility_test",
		PropertyName:       "metric_value",
		AggregationType:    types.AggregationSum,
		StartTime:          startTime,
		EndTime:            endTime,
		ExternalCustomerID: "compat_customer",
		CustomerID:         "compat_001",
		Filters: map[string][]string{
			"service": {"auth", "billing", "analytics"},
			"version": {"v1", "v2"},
		},
	}

	filterGroups := []events.FilterGroup{
		{
			ID:       "prod_group",
			Priority: 2,
			Filters: map[string][]string{
				"environment": {"production"},
			},
		},
		{
			ID:       "staging_group",
			Priority: 1,
			Filters: map[string][]string{
				"environment": {"staging"},
			},
		},
	}

	// Test that the interface works as expected
	qb := NewQueryBuilder()

	// These method calls should work without changes
	result1 := qb.WithBaseFilters(ctx, params)
	assert.NotNil(t, result1)
	assert.Equal(t, qb, result1, "WithBaseFilters should return the same instance")

	result2 := qb.WithFilterGroups(ctx, filterGroups)
	assert.NotNil(t, result2)
	assert.Equal(t, qb, result2, "WithFilterGroups should return the same instance")

	result3 := qb.WithAggregation(ctx, types.AggregationSum, "metric_value")
	assert.NotNil(t, result3)
	assert.Equal(t, qb, result3, "WithAggregation should return the same instance")

	// Build should return query and args
	query, args := qb.Build()
	assert.NotEmpty(t, query, "Query should not be empty")
	assert.NotEmpty(t, args, "Args should not be empty")

	// Query should be valid SQL structure (same as before)
	assert.Contains(t, query, "WITH")
	assert.Contains(t, query, "SELECT")
	assert.Contains(t, query, "FROM")
	assert.Contains(t, query, "GROUP BY")
	assert.Contains(t, query, "ORDER BY")
}
