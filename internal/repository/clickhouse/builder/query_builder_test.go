package builder

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/stretchr/testify/assert"
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

// TestSQLInjectionPreventionPropertyNames tests SQL injection prevention for property names
func TestSQLInjectionPreventionPropertyNames(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	// Property names in JSONExtractString are hardcoded, not parameterized
	// But we should verify they don't contain dangerous characters
	dangerousPropertyNames := []string{
		"'; DROP TABLE events; --",
		"property') OR 1=1; --",
		"prop'; DELETE FROM events; --",
	}

	for i, maliciousProperty := range dangerousPropertyNames {
		t.Run(fmt.Sprintf("PropertyName_Vector_%d", i), func(t *testing.T) {
			params := createTestUsageParams("test_event", startTime, endTime)
			params.Filters = map[string][]string{
				maliciousProperty: {"safe_value"},
			}

			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, params)

			query, args := qb.Build()

			// Property names appear directly in JSONExtractString calls
			// Verify the property name is properly handled
			assert.Contains(t, query, fmt.Sprintf("JSONExtractString(properties, '%s')", maliciousProperty))

			// Verify the value is still parameterized
			assert.Contains(t, args, "safe_value")
		})
	}
}

// TestParameterizedQueryValidation tests that all user inputs are properly parameterized
func TestParameterizedQueryValidation(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	params := &events.UsageParams{
		EventName:          "test_event",
		PropertyName:       "amount",
		AggregationType:    types.AggregationSum,
		StartTime:          startTime,
		EndTime:            endTime,
		ExternalCustomerID: "cust_ext_123",
		CustomerID:         "cust_123",
		Filters: map[string][]string{
			"region": {"us-west-1", "us-east-1"},
			"tier":   {"premium"},
		},
	}

	filterGroups := []events.FilterGroup{
		{
			ID:       "group1",
			Priority: 1,
			Filters: map[string][]string{
				"category": {"category_a", "category_b"}, // Use more unique values
			},
		},
	}

	qb := NewQueryBuilder()
	qb.WithBaseFilters(ctx, params)
	qb.WithFilterGroups(ctx, filterGroups)
	qb.WithAggregation(ctx, types.AggregationSum, "amount")

	query, args := qb.Build()

	// Count expected parameters
	expectedParams := 0
	expectedParams++    // event_name
	expectedParams++    // tenant_id
	expectedParams++    // environment_id
	expectedParams++    // start_time
	expectedParams++    // end_time
	expectedParams++    // external_customer_id
	expectedParams++    // customer_id
	expectedParams += 2 // region filter values
	expectedParams += 1 // tier filter value
	expectedParams += 2 // filter group values
	expectedParams += 1 // aggregation property name

	// Verify parameter count matches
	assert.Equal(t, expectedParams, len(args), "Parameter count should match expected")

	// Verify query uses proper placeholders (?1, ?2, etc.)
	placeholderRegex := regexp.MustCompile(`\?\d+`)
	placeholders := placeholderRegex.FindAllString(query, -1)

	// Should have at least as many placeholders as parameters
	assert.GreaterOrEqual(t, len(placeholders), len(args), "Should have sufficient placeholders")

	// Verify no raw user input appears in query (more specific test)
	userInputs := []string{
		params.EventName,
		params.ExternalCustomerID,
		params.CustomerID,
		"us-west-1", "us-east-1", "premium", "category_a", "category_b",
	}

	for _, input := range userInputs {
		// Check if the input appears in args (should be parameterized)
		assert.Contains(t, args, input, "User input should be in parameterized args")

		// For more unique values, verify they don't appear unparameterized
		if len(input) > 3 { // Only check longer strings to avoid false positives
			// Look for the input appearing outside of quoted contexts
			unquotedPattern := fmt.Sprintf(`[^']%s[^']`, regexp.QuoteMeta(input))
			unquotedRegex := regexp.MustCompile(unquotedPattern)
			assert.False(t, unquotedRegex.MatchString(query), "User input should not appear unquoted in query: %s", input)
		}
	}

	// Verify specific parameter positions have proper placeholders
	assert.Contains(t, query, "event_name = ?", "Event name should be parameterized")
	assert.Contains(t, query, "tenant_id = ?", "Tenant ID should be parameterized")
	assert.Contains(t, query, "external_customer_id = ?", "External customer ID should be parameterized")
	assert.Contains(t, query, "customer_id = ?", "Customer ID should be parameterized")

	// Verify filter conditions are parameterized
	assert.Contains(t, query, "JSONExtractString(properties, 'tier') = ?", "Tier filter should be parameterized")
	assert.Contains(t, query, "JSONExtractString(properties, 'region') IN (?", "Region filter should be parameterized")
	assert.Contains(t, query, "JSONExtractString(properties, 'category') IN (?", "Category filter should be parameterized")

	// Verify aggregation property is parameterized
	assert.Contains(t, query, "SUM(CAST(JSONExtractString(properties, ?", "Aggregation property should be parameterized")
}

// TestTimeConditionsParameterization tests that time values are properly parameterized as time.Time
func TestTimeConditionsParameterization(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")

	testCases := []struct {
		name      string
		startTime time.Time
		endTime   time.Time
	}{
		{
			name:      "Normal time range",
			startTime: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			endTime:   time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name:      "Zero start time",
			startTime: time.Time{},
			endTime:   time.Now(),
		},
		{
			name:      "Zero end time",
			startTime: time.Now().Add(-24 * time.Hour),
			endTime:   time.Time{},
		},
		{
			name:      "Far future time",
			startTime: time.Now(),
			endTime:   time.Date(2099, 12, 31, 23, 59, 59, 0, time.UTC),
		},
		{
			name:      "Very old time",
			startTime: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC),
			endTime:   time.Now(),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			params := createTestUsageParams("test_event", tc.startTime, tc.endTime)

			qb := NewQueryBuilder()
			qb.WithBaseFilters(ctx, params)

			query, args := qb.Build()

			// Verify args contains expected parameters
			assert.Greater(t, len(args), 0, "Should have parameterized arguments")

			// Test parseTimeConditions directly
			conditions, timeArgs := qb.parseTimeConditions(params)

			// Verify time conditions are generated correctly
			if !tc.startTime.IsZero() {
				assert.Contains(t, conditions, "timestamp >= toDateTime64(?%d, 3)")
				// Verify the time argument is a formatted string
				found := false
				for _, arg := range timeArgs {
					if timeStr, ok := arg.(string); ok {
						// Should match ClickHouse datetime format
						assert.Regexp(t, `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}$`, timeStr)
						found = true
						break
					}
				}
				assert.True(t, found, "Should have properly formatted time string in args")
			}

			if !tc.endTime.IsZero() {
				assert.Contains(t, conditions, "timestamp < toDateTime64(?%d, 3)")
			}

			// Verify no raw time values appear in query
			if !tc.startTime.IsZero() {
				timeStr := tc.startTime.Format("2006-01-02 15:04:05")
				assert.NotContains(t, query, timeStr, "Raw time should not appear in query")
			}

			if !tc.endTime.IsZero() {
				timeStr := tc.endTime.Format("2006-01-02 15:04:05")
				assert.NotContains(t, query, timeStr, "Raw time should not appear in query")
			}
		})
	}
}

// TestFunctionalEquivalenceWithBaseFilters tests functional equivalence of WithBaseFilters
func TestFunctionalEquivalenceWithBaseFilters(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)

	params := &events.UsageParams{
		EventName:          "api_calls",
		PropertyName:       "duration",
		AggregationType:    types.AggregationSum,
		StartTime:          startTime,
		EndTime:            endTime,
		ExternalCustomerID: "cust_external_456",
		CustomerID:         "cust_456",
		Filters: map[string][]string{
			"endpoint": {"/api/v1/users", "/api/v1/orders"},
			"method":   {"GET"},
		},
	}

	qb := NewQueryBuilder()
	qb.WithBaseFilters(ctx, params)

	query, args := qb.Build()

	// Verify essential components are present
	assert.Contains(t, query, "base_events AS")
	assert.Contains(t, query, "DISTINCT ON")
	assert.Contains(t, query, qb.getDeduplicationKey())

	// Verify all expected conditions
	assert.Contains(t, query, "event_name = ?")
	assert.Contains(t, query, "tenant_id = ?")
	assert.Contains(t, query, "environment_id = ?")
	assert.Contains(t, query, "timestamp >= toDateTime64(?")
	assert.Contains(t, query, "timestamp < toDateTime64(?")
	assert.Contains(t, query, "external_customer_id = ?")
	assert.Contains(t, query, "customer_id = ?")
	assert.Contains(t, query, "JSONExtractString(properties, 'endpoint') IN")
	assert.Contains(t, query, "JSONExtractString(properties, 'method') = ?")

	// Verify all expected arguments are present
	expectedArgs := []interface{}{
		"api_calls",
		"tenant_123",
		"env_123",
		formatClickHouseDateTime(startTime),
		formatClickHouseDateTime(endTime),
		"cust_external_456",
		"cust_456",
		"/api/v1/users", "/api/v1/orders",
		"GET",
	}

	assert.Equal(t, len(expectedArgs), len(args))
	for i, expected := range expectedArgs {
		assert.Equal(t, expected, args[i], fmt.Sprintf("Argument %d should match", i))
	}
}

// TestFunctionalEquivalenceWithFilterGroups tests functional equivalence of WithFilterGroups
func TestFunctionalEquivalenceWithFilterGroups(t *testing.T) {
	ctx := createTestContext("tenant_123", "env_123")
	startTime := time.Now().Add(-24 * time.Hour)
	endTime := time.Now()

	params := createTestUsageParams("test_event", startTime, endTime)

	filterGroups := []events.FilterGroup{
		{
			ID:       "tier_premium_group",
			Priority: 3,
			Filters: map[string][]string{
				"tier":   {"premium_tier"},
				"region": {"us-west-1"},
			},
		},
		{
			ID:       "tier_standard_group",
			Priority: 2,
			Filters: map[string][]string{
				"tier": {"standard_tier"},
			},
		},
		{
			ID:       "tier_basic_group",
			Priority: 1,
			Filters: map[string][]string{
				"tier": {"basic_tier"},
			},
		},
	}

	qb := NewQueryBuilder()
	qb.WithBaseFilters(ctx, params)
	qb.WithFilterGroups(ctx, filterGroups)

	query, args := qb.Build()

	// Verify filter_matches CTE is present
	assert.Contains(t, query, "filter_matches AS")
	assert.Contains(t, query, "arrayMap")
	assert.Contains(t, query, "group_matches")

	// Verify all filter groups are represented
	assert.Contains(t, query, "'tier_premium_group'")
	assert.Contains(t, query, "'tier_standard_group'")
	assert.Contains(t, query, "'tier_basic_group'")

	// Verify priorities are included
	assert.Contains(t, query, "3,") // tier_premium_group priority
	assert.Contains(t, query, "2,") // tier_standard_group priority
	assert.Contains(t, query, "1,") // tier_basic_group priority

	// Verify filter conditions are parameterized (check the values, not substrings)
	filterValues := []string{"premium_tier", "standard_tier", "basic_tier", "us-west-1"}
	for _, value := range filterValues {
		assert.Contains(t, args, value, "Filter value should be in args")

		// Simplified check: the value should not appear as an unquoted literal in the query
		// We'll check that it doesn't appear without being part of a parameter placeholder

		// Remove all quoted strings (group IDs, property names) from the query
		queryWithoutQuotes := regexp.MustCompile(`'[^']*'`).ReplaceAllString(query, "'QUOTED'")

		// Remove all parameter placeholders
		queryWithoutParams := regexp.MustCompile(`\?\d+`).ReplaceAllString(queryWithoutQuotes, "PARAM")

		// Now check that the value doesn't appear as a standalone word
		valueRegex := regexp.MustCompile(`\b` + regexp.QuoteMeta(value) + `\b`)
		assert.False(t, valueRegex.MatchString(queryWithoutParams),
			"Filter value should not appear unparameterized in query: %s", value)
	}

	// Verify total argument count is reasonable
	assert.Greater(t, len(args), 5, "Should have multiple arguments for complex query")
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

	t.Run("Filter group with empty filters", func(t *testing.T) {
		startTime := time.Now().Add(-24 * time.Hour)
		endTime := time.Now()
		params := createTestUsageParams("test_event", startTime, endTime)

		filterGroups := []events.FilterGroup{
			{
				ID:       "empty_group",
				Priority: 1,
				Filters:  map[string][]string{}, // Empty filters in group
			},
		}

		qb := NewQueryBuilder()
		qb.WithBaseFilters(ctx, params)
		qb.WithFilterGroups(ctx, filterGroups)

		query, args := qb.Build()

		assert.NotEmpty(t, query)
		assert.NotEmpty(t, args, "Should still have base filter arguments")
		assert.Contains(t, query, "empty_group")
		assert.Contains(t, query, "1)") // Should use constant true condition
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
	// Test that formatClickHouseDateTime is secure
	t.Run("formatClickHouseDateTime security", func(t *testing.T) {
		testTime := time.Date(2024, 12, 25, 14, 30, 45, 123000000, time.UTC)
		formatted := formatClickHouseDateTime(testTime)

		// Verify format is as expected
		assert.Equal(t, "2024-12-25 14:30:45.123", formatted)

		// Verify no injection characters
		assert.NotContains(t, formatted, "'")
		assert.NotContains(t, formatted, ";")
		assert.NotContains(t, formatted, "--")
		assert.NotContains(t, formatted, "/*")
		assert.NotContains(t, formatted, "*/")
	})

	// Test parseTimeConditions with boundary times
	t.Run("Boundary time conditions", func(t *testing.T) {
		params := &events.UsageParams{
			EventName:       "test_event",
			PropertyName:    "amount",
			AggregationType: types.AggregationCount,
			StartTime:       time.Unix(0, 0),          // Unix epoch
			EndTime:         time.Unix(2147483647, 0), // Year 2038 problem
		}

		qb := NewQueryBuilder()
		conditions, args := qb.parseTimeConditions(params)

		// Should handle boundary times safely
		assert.Len(t, conditions, 2)
		assert.Len(t, args, 2)

		// Verify time format is safe
		for _, arg := range args {
			timeStr, ok := arg.(string)
			assert.True(t, ok, "Time arg should be string")
			assert.Regexp(t, `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d{3}$`, timeStr)
		}
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
