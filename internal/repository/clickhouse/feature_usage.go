package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type FeatureUsageRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewFeatureUsageRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) events.FeatureUsageRepository {
	return &FeatureUsageRepository{
		store:  store,
		logger: logger,
	}
}

// InsertProcessedEvent inserts a single processed event
func (r *FeatureUsageRepository) InsertProcessedEvent(ctx context.Context, event *events.FeatureUsage) error {
	query := `
		INSERT INTO feature_usage (
			id, tenant_id, external_customer_id, customer_id, event_name, source, 
			timestamp, ingested_at, properties, environment_id,
			subscription_id, sub_line_item_id, price_id, meter_id, feature_id, period_id,
			unique_hash, qty_total, sign
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
	`

	propertiesJSON, err := json.Marshal(event.Properties)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to marshal event properties").
			WithReportableDetails(map[string]interface{}{
				"event_id": event.ID,
			}).
			Mark(ierr.ErrValidation)
	}

	// Default sign to 1 if not set
	sign := event.Sign
	if sign == 0 {
		sign = 1
	}

	args := []interface{}{
		event.ID,
		event.TenantID,
		event.ExternalCustomerID,
		event.CustomerID,
		event.EventName,
		event.Source,
		event.Timestamp,
		event.IngestedAt,
		string(propertiesJSON),
		event.EnvironmentID,
		event.SubscriptionID,
		event.SubLineItemID,
		event.PriceID,
		event.MeterID,
		event.FeatureID,
		event.PeriodID,
		event.UniqueHash,
		event.QtyTotal,
		sign,
	}

	err = r.store.GetConn().Exec(ctx, query, args...)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to insert processed event").
			WithReportableDetails(map[string]interface{}{
				"event_id": event.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// BulkInsertProcessedEvents inserts multiple processed events
func (r *FeatureUsageRepository) BulkInsertProcessedEvents(ctx context.Context, events []*events.FeatureUsage) error {
	if len(events) == 0 {
		return nil
	}

	// Split events in batches of 100
	eventsBatches := lo.Chunk(events, 100)

	for _, eventsBatch := range eventsBatches {
		// Prepare batch statement
		batch, err := r.store.GetConn().PrepareBatch(ctx, `
			INSERT INTO feature_usage (
				id, tenant_id, external_customer_id, customer_id, event_name, source, 
				timestamp, ingested_at, properties, environment_id,
				subscription_id, sub_line_item_id, price_id, meter_id, feature_id, period_id,
				unique_hash, qty_total, sign
			)
		`)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to prepare batch for feature usage").
				Mark(ierr.ErrDatabase)
		}

		for _, event := range eventsBatch {
			propertiesJSON, err := json.Marshal(event.Properties)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to marshal event properties").
					WithReportableDetails(map[string]interface{}{
						"event_id": event.ID,
					}).
					Mark(ierr.ErrValidation)
			}

			// Default sign to 1 if not set
			sign := event.Sign
			if sign == 0 {
				sign = 1
			}

			err = batch.Append(
				event.ID,
				event.TenantID,
				event.ExternalCustomerID,
				event.CustomerID,
				event.EventName,
				event.Source,
				event.Timestamp,
				event.IngestedAt,
				string(propertiesJSON),
				event.EnvironmentID,
				event.SubscriptionID,
				event.SubLineItemID,
				event.PriceID,
				event.MeterID,
				event.FeatureID,
				event.PeriodID,
				event.UniqueHash,
				event.QtyTotal,
				sign,
			)

			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to append feature usage to batch").
					WithReportableDetails(map[string]interface{}{
						"event_id": event.ID,
					}).
					Mark(ierr.ErrDatabase)
			}
		}

		// Send batch
		if err := batch.Send(); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to execute batch insert for feature usage").
				WithReportableDetails(map[string]interface{}{
					"event_count": len(events),
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	return nil
}

// GetProcessedEvents retrieves processed events based on the provided parameters
func (r *FeatureUsageRepository) GetProcessedEvents(ctx context.Context, params *events.GetProcessedEventsParams) ([]*events.FeatureUsage, uint64, error) {
	query := `
		SELECT 
			id, tenant_id, external_customer_id, customer_id, event_name, source, 
			timestamp, ingested_at, properties, processed_at, environment_id,
			subscription_id, sub_line_item_id, price_id, meter_id, feature_id, period_id,
			unique_hash, qty_total,version, sign, processing_lag_ms
		FROM feature_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND timestamp >= ?
		AND timestamp <= ?
	`

	countQuery := `
		SELECT COUNT(*)
		FROM feature_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND timestamp >= ?
		AND timestamp <= ?
	`

	args := []interface{}{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.StartTime, params.EndTime}
	countArgs := []interface{}{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), params.StartTime, params.EndTime}

	// Add filters
	if params.CustomerID != "" {
		query += " AND customer_id = ?"
		countQuery += " AND customer_id = ?"
		args = append(args, params.CustomerID)
		countArgs = append(countArgs, params.CustomerID)
	}

	if params.SubscriptionID != "" {
		query += " AND subscription_id = ?"
		countQuery += " AND subscription_id = ?"
		args = append(args, params.SubscriptionID)
		countArgs = append(countArgs, params.SubscriptionID)
	}

	if params.MeterID != "" {
		query += " AND meter_id = ?"
		countQuery += " AND meter_id = ?"
		args = append(args, params.MeterID)
		countArgs = append(countArgs, params.MeterID)
	}

	if params.FeatureID != "" {
		query += " AND feature_id = ?"
		countQuery += " AND feature_id = ?"
		args = append(args, params.FeatureID)
		countArgs = append(countArgs, params.FeatureID)
	}

	if params.PriceID != "" {
		query += " AND price_id = ?"
		countQuery += " AND price_id = ?"
		args = append(args, params.PriceID)
		countArgs = append(countArgs, params.PriceID)
	}

	// Add FINAL modifier to handle ReplacingMergeTree
	query += " FINAL"
	countQuery += " FINAL"

	// Apply pagination
	if params.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, params.Limit)

		if params.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, params.Offset)
		}
	}

	// Execute count query if requested
	var totalCount uint64 = 0
	if params.CountTotal {
		err := r.store.GetConn().QueryRow(ctx, countQuery, countArgs...).Scan(&totalCount)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to count processed events").
				Mark(ierr.ErrDatabase)
		}
	}

	// Execute main query
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to query processed events").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var eventsList []*events.FeatureUsage
	for rows.Next() {
		var event events.FeatureUsage
		var propertiesJSON string

		err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.ExternalCustomerID,
			&event.CustomerID,
			&event.EventName,
			&event.Source,
			&event.Timestamp,
			&event.IngestedAt,
			&propertiesJSON,
			&event.ProcessedAt,
			&event.EnvironmentID,
			&event.SubscriptionID,
			&event.SubLineItemID,
			&event.PriceID,
			&event.MeterID,
			&event.FeatureID,
			&event.PeriodID,
			&event.UniqueHash,
			&event.QtyTotal,
			&event.Version,
			&event.Sign,
			&event.ProcessingLagMs,
		)
		if err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to scan processed event").
				Mark(ierr.ErrDatabase)
		}

		// Parse properties
		if propertiesJSON != "" {
			if err := json.Unmarshal([]byte(propertiesJSON), &event.Properties); err != nil {
				return nil, 0, ierr.WithError(err).
					WithHint("Failed to unmarshal properties").
					Mark(ierr.ErrValidation)
			}
		} else {
			event.Properties = make(map[string]interface{})
		}

		eventsList = append(eventsList, &event)
	}

	return eventsList, totalCount, nil
}

// IsDuplicate checks if an event with the given unique hash already exists
func (r *FeatureUsageRepository) IsDuplicate(ctx context.Context, subscriptionID, meterID string, periodID uint64, uniqueHash string) (bool, error) {
	query := `
		SELECT 1 
		FROM feature_usage 
		WHERE subscription_id = ? 
		AND meter_id = ? 
		AND period_id = ? 
		AND unique_hash = ? 
		LIMIT 1
	`

	var exists int
	err := r.store.GetConn().QueryRow(ctx, query, subscriptionID, meterID, periodID, uniqueHash).Scan(&exists)
	if err != nil {
		// If no rows, it means no duplicate
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, ierr.WithError(err).
			WithHint("Failed to check for duplicate event").
			Mark(ierr.ErrDatabase)
	}

	return exists == 1, nil
}

// GetDetailedUsageAnalytics provides comprehensive usage analytics with filtering, grouping, and time-series data
func (r *FeatureUsageRepository) GetDetailedUsageAnalytics(ctx context.Context, params *events.UsageAnalyticsParams, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	span := StartRepositorySpan(ctx, "processed_event", "get_detailed_usage_analytics", map[string]interface{}{
		"external_customer_id":   params.ExternalCustomerID,
		"feature_ids_count":      len(params.FeatureIDs),
		"sources_count":          len(params.Sources),
		"property_filters_count": len(params.PropertyFilters),
	})
	defer FinishSpan(span)

	// Set default start/end times if not provided
	if params.EndTime.IsZero() {
		params.EndTime = time.Now().UTC()
	}

	if params.StartTime.IsZero() {
		// Default to last 6 hours if not specified
		params.StartTime = params.EndTime.Add(-6 * time.Hour)
	}

	// Set default group by if not provided
	if len(params.GroupBy) == 0 {
		params.GroupBy = []string{"feature_id"}
	}

	// Validate group by values - now supports properties.* fields
	for _, groupBy := range params.GroupBy {
		if groupBy != "feature_id" && groupBy != "source" && !strings.HasPrefix(groupBy, "properties.") {
			return nil, ierr.NewError("invalid group_by value").
				WithHint("Valid group_by values are 'feature_id', 'source', or 'properties.<field_name>'").
				WithReportableDetails(map[string]interface{}{
					"group_by": params.GroupBy,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// Use the maxBucketFeatures passed from the service layer

	// Now we'll handle the two types of features separately:
	// 1. MAX with bucket features - use bucket-based aggregation for totals, window-based for points
	// 2. Other features - use standard SUM aggregation

	var allResults []*events.DetailedUsageAnalytic

	// Handle MAX with bucket features
	if len(maxBucketFeatures) > 0 {
		maxBucketResults, err := r.getMaxBucketAnalytics(ctx, params, maxBucketFeatures)
		if err != nil {
			SetSpanError(span, err)
			return nil, err
		}
		allResults = append(allResults, maxBucketResults...)
	}

	// Handle other features (non-MAX with bucket)
	otherFeatureIDs := r.getOtherFeatureIDs(params.FeatureIDs, maxBucketFeatures)

	// Only process other features if we have some to process
	if len(otherFeatureIDs) > 0 || len(params.FeatureIDs) == 0 {
		otherParams := *params
		if len(otherFeatureIDs) > 0 {
			// We have specific other features to process
			otherParams.FeatureIDs = otherFeatureIDs
		} else if len(params.FeatureIDs) == 0 {
			// No specific features requested, but we need to exclude MAX bucket features
			// from the standard processing
			// Set empty feature IDs to process all, but the standard analytics will handle
			// the filtering internally
			otherParams.FeatureIDs = []string{}
		}

		otherResults, err := r.getStandardAnalytics(ctx, &otherParams, maxBucketFeatures)
		if err != nil {
			SetSpanError(span, err)
			return nil, err
		}
		allResults = append(allResults, otherResults...)
	}

	SetSpanSuccess(span)
	return allResults, nil
}

// getOtherFeatureIDs returns feature IDs that are not MAX with bucket features
func (r *FeatureUsageRepository) getOtherFeatureIDs(requestedFeatureIDs []string, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo) []string {
	// If no specific features requested, we need to handle all features
	// We'll return empty slice to indicate "handle all features" in standard way
	if len(requestedFeatureIDs) == 0 {
		return []string{} // Empty slice means "handle all features in standard way"
	}

	otherFeatureIDs := make([]string, 0)
	for _, featureID := range requestedFeatureIDs {
		if _, isMaxBucket := maxBucketFeatures[featureID]; !isMaxBucket {
			otherFeatureIDs = append(otherFeatureIDs, featureID)
		}
	}
	return otherFeatureIDs
}

// getStandardAnalytics handles analytics for non-MAX with bucket features
func (r *FeatureUsageRepository) getStandardAnalytics(ctx context.Context, params *events.UsageAnalyticsParams, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	// Initialize query parameters with the standard parameters that will be added later
	// This ensures they're always in the right order
	queryParams := []interface{}{
		params.TenantID,
		params.EnvironmentID,
		params.CustomerID,
		params.StartTime,
		params.EndTime,
	}

	// Add group by columns based on params.GroupBy
	// Always include feature_id for cost calculation, then add requested grouping dimensions
	groupByColumns := []string{"feature_id"}       // Always include feature_id for cost calculation
	groupByColumnAliases := []string{"feature_id"} // for SELECT clause
	groupByFieldMapping := make(map[string]string) // maps original field to column alias
	groupByFieldMapping["feature_id"] = "feature_id"

	// Add the requested grouping dimensions
	for _, groupBy := range params.GroupBy {
		switch {
		case groupBy == "feature_id":
			// Already included above
		case groupBy == "source":
			groupByColumns = append(groupByColumns, "source")
			groupByColumnAliases = append(groupByColumnAliases, "source")
			groupByFieldMapping["source"] = "source"
		case strings.HasPrefix(groupBy, "properties."):
			// Extract property name from "properties.field_name"
			propertyName := strings.TrimPrefix(groupBy, "properties.")
			if propertyName != "" {
				// Create alias like "prop_org_id" for "properties.org_id"
				alias := "prop_" + strings.ReplaceAll(propertyName, ".", "_")
				sqlExpression := fmt.Sprintf("JSONExtractString(properties, '%s') AS %s", propertyName, alias)
				groupByColumns = append(groupByColumns, fmt.Sprintf("JSONExtractString(properties, '%s')", propertyName))
				groupByColumnAliases = append(groupByColumnAliases, sqlExpression)
				groupByFieldMapping[groupBy] = alias
			}
		}
	}

	// Base query for aggregates - fetch all aggregation types for each feature
	selectColumns := []string{}
	if len(groupByColumnAliases) > 0 {
		selectColumns = append(selectColumns, strings.Join(groupByColumnAliases, ", ")) // group by columns with aliases
	}
	selectColumns = append(selectColumns,
		"SUM(qty_total * sign) AS total_usage",
		"MAX(qty_total * sign) AS max_usage",
		"argMax(qty_total, timestamp) AS latest_usage",
		"COUNT(DISTINCT unique_hash) AS count_unique_usage",
		"COUNT(DISTINCT id) AS event_count", // Count distinct event IDs, not rows
	)

	aggregateQuery := fmt.Sprintf(`
		SELECT 
			%s
		FROM feature_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND customer_id = ?
		AND timestamp >= ?
		AND timestamp < ?
		AND sign != 0
	`, strings.Join(selectColumns, ",\n\t\t\t"))

	// Add filters for feature_ids
	filterParams := []interface{}{}
	if len(params.FeatureIDs) > 0 {
		placeholders := make([]string, len(params.FeatureIDs))
		for i := range params.FeatureIDs {
			placeholders[i] = "?"
			filterParams = append(filterParams, params.FeatureIDs[i])
		}
		aggregateQuery += " AND feature_id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	if len(maxBucketFeatures) > 0 {
		// If no specific feature IDs but we have MAX bucket features,
		// exclude them from standard processing
		maxBucketFeatureIDs := make([]string, 0, len(maxBucketFeatures))
		for featureID := range maxBucketFeatures {
			maxBucketFeatureIDs = append(maxBucketFeatureIDs, featureID)
		}
		placeholders := make([]string, len(maxBucketFeatureIDs))
		for i := range maxBucketFeatureIDs {
			placeholders[i] = "?"
			filterParams = append(filterParams, maxBucketFeatureIDs[i])
		}
		aggregateQuery += " AND feature_id NOT IN (" + strings.Join(placeholders, ", ") + ")"
	}

	// Add filters for sources
	if len(params.Sources) > 0 {
		placeholders := make([]string, len(params.Sources))
		for i := range params.Sources {
			placeholders[i] = "?"
			filterParams = append(filterParams, params.Sources[i])
		}
		aggregateQuery += " AND source IN (" + strings.Join(placeholders, ", ") + ")"
	}

	// add properties filters
	if len(params.PropertyFilters) > 0 {
		for property, values := range params.PropertyFilters {
			if len(values) > 0 {
				if len(values) == 1 {
					aggregateQuery += " AND JSONExtractString(properties, ?) = ?"
					filterParams = append(filterParams, property, values[0])
				} else {
					placeholders := make([]string, len(values))
					for i := range values {
						placeholders[i] = "?"
					}
					aggregateQuery += " AND JSONExtractString(properties, ?) IN (" + strings.Join(placeholders, ",") + ")"
					filterParams = append(filterParams, property)
					// Now append all values after the property
					for _, v := range values {
						filterParams = append(filterParams, v)
					}
				}
			}
		}
	}

	// Add all filter parameters after the standard parameters
	queryParams = append(queryParams, filterParams...)

	// Add group by clause
	if len(groupByColumns) > 0 {
		aggregateQuery += " GROUP BY " + strings.Join(groupByColumns, ", ")
	}

	r.logger.Debugw("executing detailed usage analytics query",
		"query", aggregateQuery,
		"params", queryParams,
		"group_by", params.GroupBy,
		"property_filters", params.PropertyFilters,
	)

	// Execute the query
	rows, err := r.store.GetConn().Query(ctx, aggregateQuery, queryParams...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute usage analytics query").
			WithReportableDetails(map[string]interface{}{
				"customer_id": params.CustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	// Process the results
	results := []*events.DetailedUsageAnalytic{}

	// Read the rows
	for rows.Next() {
		analytics := &events.DetailedUsageAnalytic{
			Points: []events.UsageAnalyticPoint{},
		}

		// Initialize properties map
		analytics.Properties = make(map[string]string)

		// Scan the row based on group by columns
		// The actual number of group by columns is determined by the query structure
		// which includes feature_id + all requested grouping dimensions
		totalGroupByColumns := len(groupByColumns) // This matches the actual GROUP BY columns in the query
		expectedColumns := totalGroupByColumns + 5 // +5 for sum_usage, max_usage, latest_usage, count_unique_usage, event_count
		scanArgs := make([]interface{}, expectedColumns)

		// Prepare scan targets: all group by columns
		scanTargets := make([]string, totalGroupByColumns)
		for i := range scanTargets {
			scanArgs[i] = &scanTargets[i]
		}

		// Scan the aggregate values
		scanArgs[totalGroupByColumns] = &analytics.TotalUsage
		scanArgs[totalGroupByColumns+1] = &analytics.MaxUsage
		scanArgs[totalGroupByColumns+2] = &analytics.LatestUsage
		scanArgs[totalGroupByColumns+3] = &analytics.CountUniqueUsage
		scanArgs[totalGroupByColumns+4] = &analytics.EventCount

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan analytics row").
				WithReportableDetails(map[string]interface{}{
					"customer_id": params.CustomerID,
				}).
				Mark(ierr.ErrDatabase)
		}

		// Populate analytics fields based on the group by columns structure
		// We need to map the scanned values to the correct fields based on the groupByColumns order
		scanIndex := 0

		// Process each group by column in the order they appear in the query
		for _, groupByCol := range groupByColumns {
			value := scanTargets[scanIndex]

			// Map the column to the appropriate field
			switch groupByCol {
			case "feature_id":
				analytics.FeatureID = value
			case "source":
				analytics.Source = value
			default:
				// For properties fields, extract the property name from the JSONExtractString expression
				if strings.HasPrefix(groupByCol, "JSONExtractString(properties, '") {
					// Extract property name from "JSONExtractString(properties, 'property_name')"
					start := len("JSONExtractString(properties, '")
					end := strings.Index(groupByCol[start:], "'")
					if end > 0 {
						propertyName := groupByCol[start : start+end]
						analytics.Properties[propertyName] = value
					}
				}
			}
			scanIndex++
		}

		// If we need time-series data and a window size is specified, fetch the points
		if params.WindowSize != "" {
			points, err := r.getAnalyticsPoints(ctx, params, analytics)
			if err != nil {
				return nil, err
			}
			analytics.Points = points
		}

		results = append(results, analytics)
	}

	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Error iterating analytics rows").
			WithReportableDetails(map[string]interface{}{
				"customer_id": params.CustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return results, nil
}

// getMaxBucketAnalytics handles analytics for MAX with bucket features
func (r *FeatureUsageRepository) getMaxBucketAnalytics(ctx context.Context, params *events.UsageAnalyticsParams, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	// For MAX with bucket features, we need to:
	// 1. Calculate totals using bucket-based aggregation (meter's bucket size)
	// 2. Calculate time series points using request window size

	var allResults []*events.DetailedUsageAnalytic

	// Process each MAX with bucket feature separately since they may have different bucket sizes
	for featureID, featureInfo := range maxBucketFeatures {
		// Create a copy of params for this specific feature
		featureParams := *params
		featureParams.FeatureIDs = []string{featureID}

		// Get bucket-based totals
		totals, err := r.getMaxBucketTotals(ctx, &featureParams, featureInfo)
		if err != nil {
			return nil, err
		}

		// Get window-based time series points for each group
		if featureInfo.BucketSize != "" {
			// Need to get points per group to match totals
			for _, total := range totals {
				points, err := r.getMaxBucketPointsForGroup(ctx, &featureParams, featureInfo, total)
				if err != nil {
					return nil, err
				}
				total.Points = points
				allResults = append(allResults, total)
			}
		} else {
			allResults = append(allResults, totals...)
		}
	}

	return allResults, nil
}

// getMaxBucketTotals calculates totals using bucket-based aggregation for MAX features
func (r *FeatureUsageRepository) getMaxBucketTotals(ctx context.Context, params *events.UsageAnalyticsParams, featureInfo *events.MaxBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	// Build bucket window expression based on meter's bucket size
	bucketWindowExpr := r.formatWindowSize(featureInfo.BucketSize, nil)

	// Build group by columns based on request parameters
	groupByColumns := []string{"bucket_start", "feature_id"}
	innerSelectColumns := []string{"feature_id"} // For inner query (has access to properties column)
	outerSelectColumns := []string{"feature_id"} // For outer query (only has aliased columns)

	// Add grouping columns
	for _, groupBy := range params.GroupBy {
		switch groupBy {
		case "source":
			groupByColumns = append(groupByColumns, "source")
			innerSelectColumns = append(innerSelectColumns, "source")
			outerSelectColumns = append(outerSelectColumns, "source")
		case "feature_id":
			// Already included
		default:
			if strings.HasPrefix(groupBy, "properties.") {
				propertyName := strings.TrimPrefix(groupBy, "properties.")
				groupByColumns = append(groupByColumns, fmt.Sprintf("JSONExtractString(properties, '%s')", propertyName))
				innerSelectColumns = append(innerSelectColumns, fmt.Sprintf("JSONExtractString(properties, '%s') as %s", propertyName, propertyName))
				outerSelectColumns = append(outerSelectColumns, propertyName) // Just the alias
			}
		}
	}

	// Build the query for bucket-based MAX aggregation
	// For MAX with bucket, we need to:
	// 1. Find the max value within each bucket (grouped by requested fields)
	// 2. Aggregate across all buckets to get totals

	// Build inner query with filters
	innerQuery := fmt.Sprintf(`
		SELECT
			%s as bucket_start,
			%s,
			max(qty_total * sign) as bucket_max,
			argMax(qty_total, timestamp) as bucket_latest,
			count(DISTINCT unique_hash) as bucket_count_unique,
			count(DISTINCT id) as event_count
		FROM feature_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND customer_id = ?
		AND feature_id = ?
		AND timestamp >= ?
		AND timestamp < ?
		AND sign != 0`, bucketWindowExpr, strings.Join(innerSelectColumns, ", "))

	queryParams := []interface{}{
		params.TenantID,
		params.EnvironmentID,
		params.CustomerID,
		featureInfo.FeatureID,
		params.StartTime,
		params.EndTime,
	}

	// Add filters for sources to inner query
	if len(params.Sources) > 0 {
		placeholders := make([]string, len(params.Sources))
		for i := range params.Sources {
			placeholders[i] = "?"
		}
		innerQuery += " AND source IN (" + strings.Join(placeholders, ", ") + ")"
		for _, source := range params.Sources {
			queryParams = append(queryParams, source)
		}
	}

	// Add property filters to inner query
	if len(params.PropertyFilters) > 0 {
		for property, values := range params.PropertyFilters {
			if len(values) > 0 {
				if len(values) == 1 {
					innerQuery += " AND JSONExtractString(properties, ?) = ?"
					queryParams = append(queryParams, property, values[0])
				} else {
					placeholders := make([]string, len(values))
					for i := range values {
						placeholders[i] = "?"
					}
					innerQuery += " AND JSONExtractString(properties, ?) IN (" + strings.Join(placeholders, ",") + ")"
					queryParams = append(queryParams, property)
					// Now append all values after the property
					for _, v := range values {
						queryParams = append(queryParams, v)
					}
				}
			}
		}
	}

	// Complete the inner query with GROUP BY
	innerQuery += fmt.Sprintf(" GROUP BY %s", strings.Join(groupByColumns, ", "))

	// Build the complete query with CTE
	query := fmt.Sprintf(`
		WITH bucket_maxes AS (
			%s
		)
		SELECT
			%s,
			sum(bucket_max) as total_usage,
			max(bucket_max) as max_usage,
			argMax(bucket_latest, bucket_start) as latest_usage,
			sum(bucket_count_unique) as count_unique_usage,
			sum(event_count) as event_count
		FROM bucket_maxes
	`, innerQuery, strings.Join(outerSelectColumns, ", "))

	// Add GROUP BY clause
	query += " GROUP BY " + strings.Join(outerSelectColumns, ", ")

	rows, err := r.store.GetConn().Query(ctx, query, queryParams...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute MAX bucket totals query").
			WithReportableDetails(map[string]interface{}{
				"feature_id":  featureInfo.FeatureID,
				"bucket_size": featureInfo.BucketSize,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []*events.DetailedUsageAnalytic
	for rows.Next() {
		analytics := &events.DetailedUsageAnalytic{
			FeatureID:       featureInfo.FeatureID,
			MeterID:         featureInfo.MeterID,
			EventName:       featureInfo.EventName,
			AggregationType: types.AggregationMax,
			Points:          []events.UsageAnalyticPoint{},
			Properties:      make(map[string]string),
		}

		// Build scan targets dynamically based on outerSelectColumns structure
		// The query selects: outerSelectColumns + total_usage + max_usage + latest_usage + count_unique_usage + event_count
		totalSelectColumns := len(outerSelectColumns) + 5 // +5 for total_usage, max_usage, latest_usage, count_unique_usage, event_count
		scanTargets := make([]interface{}, totalSelectColumns)

		// Create string targets for all select columns
		selectValues := make([]string, len(outerSelectColumns))
		for i := range selectValues {
			scanTargets[i] = &selectValues[i]
		}

		// Add usage metrics targets
		scanTargets[len(outerSelectColumns)] = &analytics.TotalUsage
		scanTargets[len(outerSelectColumns)+1] = &analytics.MaxUsage
		scanTargets[len(outerSelectColumns)+2] = &analytics.LatestUsage
		scanTargets[len(outerSelectColumns)+3] = &analytics.CountUniqueUsage
		scanTargets[len(outerSelectColumns)+4] = &analytics.EventCount

		err := rows.Scan(scanTargets...)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan MAX bucket totals row").
				Mark(ierr.ErrDatabase)
		}

		// Populate fields based on outerSelectColumns order
		for i, selectCol := range outerSelectColumns {
			value := selectValues[i]
			switch selectCol {
			case "feature_id":
				analytics.FeatureID = value
			case "source":
				analytics.Source = value
			default:
				// For property columns, the selectCol is just the property name (alias)
				if value != "" {
					analytics.Properties[selectCol] = value
				}
			}
		}

		results = append(results, analytics)
	}

	return results, nil
}

// getMaxBucketPointsForGroup calculates time series points for a specific group
func (r *FeatureUsageRepository) getMaxBucketPointsForGroup(ctx context.Context, params *events.UsageAnalyticsParams, featureInfo *events.MaxBucketFeatureInfo, group *events.DetailedUsageAnalytic) ([]events.UsageAnalyticPoint, error) {
	// Build window expression based on request window size
	windowExpr := r.formatWindowSize(featureInfo.BucketSize, params.BillingAnchor)

	// For MAX with bucket features, we need to first get max within each bucket,
	// then aggregate those maxes within the request window
	// This version filters by the specific group's attributes (source, properties, etc.)

	// Build inner query with filters
	innerQuery := fmt.Sprintf(`
		SELECT
			%s as bucket_start,
			%s as window_start,
			max(qty_total * sign) as bucket_max,
			argMax(qty_total, timestamp) as bucket_latest,
			count(DISTINCT unique_hash) as bucket_count_unique,
			count(DISTINCT id) as event_count
		FROM feature_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND customer_id = ?
		AND feature_id = ?
		AND timestamp >= ?
		AND timestamp < ?
		AND sign != 0`, r.formatWindowSize(featureInfo.BucketSize, nil), windowExpr)

	queryParams := []interface{}{
		params.TenantID,
		params.EnvironmentID,
		params.CustomerID,
		featureInfo.FeatureID,
		params.StartTime,
		params.EndTime,
	}

	// Add filter for this specific group's source
	if group.Source != "" {
		innerQuery += " AND source = ?"
		queryParams = append(queryParams, group.Source)
	}

	// Add filters for this specific group's properties
	if len(group.Properties) > 0 {
		for propertyName, propertyValue := range group.Properties {
			if propertyValue != "" {
				innerQuery += " AND JSONExtractString(properties, ?) = ?"
				queryParams = append(queryParams, propertyName, propertyValue)
			}
		}
	}

	// Add general property filters from params
	if len(params.PropertyFilters) > 0 {
		for property, values := range params.PropertyFilters {
			if len(values) > 0 {
				if len(values) == 1 {
					innerQuery += " AND JSONExtractString(properties, ?) = ?"
					queryParams = append(queryParams, property, values[0])
				} else {
					placeholders := make([]string, len(values))
					for i := range values {
						placeholders[i] = "?"
					}
					innerQuery += " AND JSONExtractString(properties, ?) IN (" + strings.Join(placeholders, ",") + ")"
					queryParams = append(queryParams, property)
					// Now append all values after the property
					for _, v := range values {
						queryParams = append(queryParams, v)
					}
				}
			}
		}
	}

	// Complete the inner query with GROUP BY
	innerQuery += " GROUP BY bucket_start, window_start"

	// Build the complete query with CTE
	query := fmt.Sprintf(`
		WITH bucket_maxes AS (
			%s
		)
		SELECT
			window_start as timestamp,
			sum(bucket_max) as usage,
			max(bucket_max) as max_usage,
			argMax(bucket_latest, window_start) as latest_usage,
			sum(bucket_count_unique) as count_unique_usage,
			sum(event_count) as event_count
		FROM bucket_maxes
	`, innerQuery)

	// Add GROUP BY and ORDER BY clauses
	query += " GROUP BY window_start ORDER BY window_start"

	rows, err := r.store.GetConn().Query(ctx, query, queryParams...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute MAX bucket points query").
			WithReportableDetails(map[string]interface{}{
				"feature_id":  featureInfo.FeatureID,
				"bucket_size": featureInfo.BucketSize,
				"window_size": params.WindowSize,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var points []events.UsageAnalyticPoint
	for rows.Next() {
		var point events.UsageAnalyticPoint
		var timestamp time.Time

		err := rows.Scan(
			&timestamp,
			&point.Usage,
			&point.MaxUsage,
			&point.LatestUsage,
			&point.CountUniqueUsage,
			&point.EventCount,
		)
		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan MAX bucket points row").
				Mark(ierr.ErrDatabase)
		}

		point.Timestamp = timestamp
		point.Cost = decimal.Zero // Will be calculated in enrichment
		points = append(points, point)
	}

	return points, nil
}

// formatWindowSize formats window size for ClickHouse queries
func (r *FeatureUsageRepository) formatWindowSize(windowSize types.WindowSize, billingAnchor *time.Time) string {
	switch windowSize {
	case types.WindowSizeMinute:
		return "toStartOfMinute(timestamp)"
	case types.WindowSizeHour:
		return "toStartOfHour(timestamp)"
	case types.WindowSizeDay:
		return "toStartOfDay(timestamp)"
	case types.WindowSizeWeek:
		return "toStartOfWeek(timestamp)"
	case types.WindowSize15Min:
		return "toStartOfInterval(timestamp, INTERVAL 15 MINUTE)"
	case types.WindowSize30Min:
		return "toStartOfInterval(timestamp, INTERVAL 30 MINUTE)"
	case types.WindowSize3Hour:
		return "toStartOfInterval(timestamp, INTERVAL 3 HOUR)"
	case types.WindowSize6Hour:
		return "toStartOfInterval(timestamp, INTERVAL 6 HOUR)"
	case types.WindowSize12Hour:
		return "toStartOfInterval(timestamp, INTERVAL 12 HOUR)"
	case types.WindowSizeMonth:
		// Use custom monthly billing period if billing anchor is provided
		if billingAnchor != nil {
			// Extract only the day component from billing anchor for simplicity
			anchorDay := billingAnchor.Day()
			// Generate the custom monthly window expression using day-level granularity
			return fmt.Sprintf(`
				addDays(
					toStartOfMonth(addDays(timestamp, -%d)),
					%d
				)`, anchorDay-1, anchorDay-1)
		}
		return "toStartOfMonth(timestamp)"
	default:
		return "toStartOfHour(timestamp)"
	}
}

// getAnalyticsPoints fetches time-series data points for a specific analytics item
func (r *FeatureUsageRepository) getAnalyticsPoints(
	ctx context.Context,
	params *events.UsageAnalyticsParams,
	analytics *events.DetailedUsageAnalytic,
) ([]events.UsageAnalyticPoint, error) {
	// Build the time window expression based on window size
	var timeWindowExpr string

	switch params.WindowSize {
	case types.WindowSizeMinute:
		timeWindowExpr = "toStartOfMinute(timestamp)"
	case types.WindowSizeHour:
		timeWindowExpr = "toStartOfHour(timestamp)"
	case types.WindowSizeDay:
		timeWindowExpr = "toStartOfDay(timestamp)"
	case types.WindowSizeWeek:
		timeWindowExpr = "toStartOfWeek(timestamp)"
	case types.WindowSize15Min:
		timeWindowExpr = "toStartOfInterval(timestamp, INTERVAL 15 MINUTE)"
	case types.WindowSize30Min:
		timeWindowExpr = "toStartOfInterval(timestamp, INTERVAL 30 MINUTE)"
	case types.WindowSize12Hour:
		timeWindowExpr = "toStartOfInterval(timestamp, INTERVAL 12 HOUR)"
	case types.WindowSize3Hour:
		timeWindowExpr = "toStartOfInterval(timestamp, INTERVAL 3 HOUR)"
	case types.WindowSize6Hour:
		timeWindowExpr = "toStartOfInterval(timestamp, INTERVAL 6 HOUR)"
	case types.WindowSizeMonth:
		// Use custom monthly billing period if billing anchor is provided
		if params.BillingAnchor != nil {
			// Extract only the day component from billing anchor for simplicity
			anchorDay := params.BillingAnchor.Day()

			// Generate the custom monthly window expression using day-level granularity
			timeWindowExpr = fmt.Sprintf(`
				addDays(
					toStartOfMonth(addDays(timestamp, -%d)),
					%d
				)`, anchorDay-1, anchorDay-1)
		} else {
			timeWindowExpr = "toStartOfMonth(timestamp)"
		}
	default:
		// Default to hourly for unknown window sizes
		timeWindowExpr = "toStartOfHour(timestamp)"
	}

	// Build the select columns for time-series query - fetch all aggregation types
	selectColumns := []string{
		fmt.Sprintf("%s AS window_time", timeWindowExpr),
		"SUM(qty_total * sign) AS total_usage",
		"MAX(qty_total * sign) AS max_usage",
		"argMax(qty_total, timestamp) AS latest_usage",
		"COUNT(DISTINCT unique_hash) AS count_unique_usage",
		"COUNT(DISTINCT id) AS event_count", // Count distinct event IDs, not rows
	}

	// Build the query
	query := fmt.Sprintf(`
		SELECT 
			%s
		FROM feature_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND customer_id = ?
		AND timestamp >= ?
		AND timestamp < ?
		AND sign != 0
	`, strings.Join(selectColumns, ",\n\t\t\t"))

	// Add filters for the specific analytics item
	queryParams := []interface{}{
		params.TenantID,
		params.EnvironmentID,
		params.CustomerID,
		params.StartTime,
		params.EndTime,
	}

	// Add feature_id filter if present in analytics
	if analytics.FeatureID != "" {
		query += " AND feature_id = ?"
		queryParams = append(queryParams, analytics.FeatureID)
	}

	// Add source filter if present in analytics
	if analytics.Source != "" {
		query += " AND source = ?"
		queryParams = append(queryParams, analytics.Source)
	}

	// Add filters for grouped properties values
	if analytics.Properties != nil {
		for propertyName, value := range analytics.Properties {
			if value != "" {
				query += " AND JSONExtractString(properties, ?) = ?"
				queryParams = append(queryParams, propertyName, value)
			}
		}
	}

	// Add property filters
	filterParamsForTimeSeries := []interface{}{}
	if len(params.PropertyFilters) > 0 {
		for property, values := range params.PropertyFilters {
			if len(values) > 0 {
				if len(values) == 1 {
					query += " AND JSONExtractString(properties, ?) = ?"
					filterParamsForTimeSeries = append(filterParamsForTimeSeries, property, values[0])
				} else {
					placeholders := make([]string, len(values))
					for i := range values {
						placeholders[i] = "?"
					}
					query += " AND JSONExtractString(properties, ?) IN (" + strings.Join(placeholders, ",") + ")"
					filterParamsForTimeSeries = append(filterParamsForTimeSeries, property)
					// Now append all values after the property
					for _, v := range values {
						filterParamsForTimeSeries = append(filterParamsForTimeSeries, v)
					}
				}
			}
		}
	}
	queryParams = append(queryParams, filterParamsForTimeSeries...)

	// Group by the time window and order by time
	query += fmt.Sprintf(" GROUP BY %s ORDER BY window_time", timeWindowExpr)

	r.logger.Debugw("executing time-series query",
		"query", query,
		"params", queryParams,
		"feature_id", analytics.FeatureID,
		"source", analytics.Source,
		"property_filters", params.PropertyFilters,
	)

	// Execute the query
	rows, err := r.store.GetConn().Query(ctx, query, queryParams...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute time-series query").
			WithReportableDetails(map[string]interface{}{
				"customer_id": params.CustomerID,
				"feature_id":  analytics.FeatureID,
				"source":      analytics.Source,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	// Process the results
	var points []events.UsageAnalyticPoint

	for rows.Next() {
		var point events.UsageAnalyticPoint

		if err := rows.Scan(
			&point.Timestamp,
			&point.Usage,
			&point.MaxUsage,
			&point.LatestUsage,
			&point.CountUniqueUsage,
			&point.EventCount,
		); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan time-series point").
				Mark(ierr.ErrDatabase)
		}

		// Set Cost to zero since it's not calculated in this query
		point.Cost = decimal.Zero
		// Usage is already set from the query (SUM(qty_total * sign))

		points = append(points, point)
	}

	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Error iterating time-series points").
			Mark(ierr.ErrDatabase)
	}

	return points, nil
}

// GetFeatureUsageBySubscription gets usage data for a subscription using a single optimized query
func (r *FeatureUsageRepository) GetFeatureUsageBySubscription(ctx context.Context, subscriptionID, externalCustomerID, environmentID, tenantID string, startTime, endTime time.Time) (map[string]*events.UsageByFeatureResult, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "feature_usage", "get_usage_by_subscription_v2", map[string]interface{}{
		"subscription_id":      subscriptionID,
		"external_customer_id": externalCustomerID,
		"environment_id":       environmentID,
		"tenant_id":            tenantID,
		"start_time":           startTime,
		"end_time":             endTime,
	})
	defer FinishSpan(span)

	query := `
		SELECT 
			sub_line_item_id,
			feature_id,
			meter_id,
			sum(qty_total)                     AS sum_total,
			max(qty_total)                     AS max_total,
			count(DISTINCT id)                 AS count_distinct_ids,
			count(DISTINCT unique_hash)        AS count_unique_qty,
			argMax(qty_total, "timestamp")     AS latest_qty
		FROM feature_usage
		WHERE 
			subscription_id = ?
			AND external_customer_id = ?
			AND environment_id = ?
			AND tenant_id = ?
			AND "timestamp" >= ?
			AND "timestamp" < ?
		GROUP BY sub_line_item_id, feature_id, meter_id
	`

	log.Printf("Executing query: %s", query)
	log.Printf("Params: %v", []interface{}{subscriptionID, externalCustomerID, environmentID, tenantID, startTime, endTime})

	rows, err := r.store.GetConn().Query(ctx, query, subscriptionID, externalCustomerID, environmentID, tenantID, startTime, endTime)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to execute optimized subscription usage query").
			WithReportableDetails(map[string]interface{}{
				"subscription_id":      subscriptionID,
				"external_customer_id": externalCustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	results := make(map[string]*events.UsageByFeatureResult)
	for rows.Next() {
		var subLineItemID, featureID, meterID string
		var sumTotal, maxTotal, latestQty decimal.Decimal
		var countDistinctIDs, countUniqueQty uint64

		err := rows.Scan(&subLineItemID, &featureID, &meterID, &sumTotal, &maxTotal, &countDistinctIDs, &countUniqueQty, &latestQty)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan usage result").
				Mark(ierr.ErrDatabase)
		}

		results[subLineItemID] = &events.UsageByFeatureResult{
			SubLineItemID:    subLineItemID,
			FeatureID:        featureID,
			MeterID:          meterID,
			SumTotal:         sumTotal,
			MaxTotal:         maxTotal,
			CountDistinctIDs: countDistinctIDs,
			CountUniqueQty:   countUniqueQty,
			LatestQty:        latestQty,
		}
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating usage results").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.logger.Debugw("optimized subscription usage query completed",
		"subscription_id", subscriptionID,
		"feature_count", len(results))

	return results, nil
}
