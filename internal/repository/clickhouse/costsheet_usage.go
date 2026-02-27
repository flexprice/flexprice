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

type CostSheetUsageRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewCostSheetUsageRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) events.CostSheetUsageRepository {
	return &CostSheetUsageRepository{
		store:  store,
		logger: logger,
	}
}

// BulkInsertProcessedEvents inserts multiple processed events
func (r *CostSheetUsageRepository) BulkInsertProcessedEvents(ctx context.Context, events []*events.CostUsage) error {
	if len(events) == 0 {
		return nil
	}

	// Split events in batches of 100
	eventsBatches := lo.Chunk(events, 100)

	for _, eventsBatch := range eventsBatches {
		// Prepare batch statement
		batch, err := r.store.GetConn().PrepareBatch(ctx, `
			INSERT INTO costsheet_usage (
				id, tenant_id, external_customer_id, customer_id, event_name, source, 
				timestamp, ingested_at, properties, environment_id,
				costsheet_id, price_id, meter_id, feature_id,
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
				event.CostSheetID,
				event.PriceID,
				event.MeterID,
				event.FeatureID,
				event.UniqueHash,
				event.QtyTotal,
				sign,
			)

			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to append cost usage to batch").
					WithReportableDetails(map[string]interface{}{
						"event_id": event.ID,
					}).
					Mark(ierr.ErrDatabase)
			}
		}

		// Send batch
		if err := batch.Send(); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to execute batch insert for cost usage").
				WithReportableDetails(map[string]interface{}{
					"event_count": len(events),
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	return nil
}

// GetProcessedEvents retrieves processed events based on the provided parameters
func (r *CostSheetUsageRepository) GetProcessedEvents(ctx context.Context, params *events.GetCostUsageEventsParams) ([]*events.CostUsage, uint64, error) {
	query := `
		SELECT 
			id, tenant_id, external_customer_id, customer_id, event_name, source, 
			timestamp, ingested_at, properties, processed_at, environment_id,
			costsheet_id, price_id, meter_id, feature_id,
			unique_hash, qty_total,version, sign, processing_lag_ms
		FROM costsheet_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND timestamp >= ?
		AND timestamp <= ?
	`

	countQuery := `
		SELECT COUNT(*)
		FROM costsheet_usage
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

	if params.CostSheetID != "" {
		query += " AND costsheet_id = ?"
		countQuery += " AND costsheet_id = ?"
		args = append(args, params.CostSheetID)
		countArgs = append(countArgs, params.CostSheetID)
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

	var eventsList []*events.CostUsage
	for rows.Next() {
		var event events.CostUsage
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
			&event.CostSheetID,
			&event.PriceID,
			&event.MeterID,
			&event.FeatureID,
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

// GetFeatureUsageBySubscription gets usage data for a subscription using a single optimized query
func (r *CostSheetUsageRepository) GetUsageByCostSheetID(ctx context.Context, costSheetID, externalCustomerID string, startTime, endTime time.Time) (map[string]*events.UsageByCostSheetResult, error) {
	// Extract tenantID and environmentID from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "costsheet_usage", "get_usage_by_costsheet_id", map[string]interface{}{
		"costsheet_id":         costSheetID,
		"external_customer_id": externalCustomerID,
		"environment_id":       environmentID,
		"tenant_id":            tenantID,
		"start_time":           startTime,
		"end_time":             endTime,
	})
	defer FinishSpan(span)

	query := `
		SELECT 
			feature_id,
			meter_id,
			price_id,
			sum(qty_total * sign)              AS sum_total,
			max(qty_total * sign)              AS max_total,
			count(DISTINCT id)                 AS count_distinct_ids,
			count(DISTINCT unique_hash)        AS count_unique_qty,
			argMax(qty_total * sign, timestamp) AS latest_qty
		FROM costsheet_usage
		WHERE 
			costsheet_id = ?
			AND external_customer_id = ?
			AND environment_id = ?
			AND tenant_id = ?
			AND timestamp >= ?
			AND timestamp < ?
			AND sign != 0
		GROUP BY feature_id, meter_id, price_id
	`

	log.Printf("Executing query: %s", query)
	log.Printf("Params: %v", []interface{}{costSheetID, externalCustomerID, environmentID, tenantID, startTime, endTime})

	rows, err := r.store.GetConn().Query(ctx, query, costSheetID, externalCustomerID, environmentID, tenantID, startTime, endTime)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to execute optimized subscription usage query").
			WithReportableDetails(map[string]interface{}{
				"costsheet_id":         costSheetID,
				"external_customer_id": externalCustomerID,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	results := make(map[string]*events.UsageByCostSheetResult)
	for rows.Next() {
		var featureID, meterID, priceID string
		var sumTotal, maxTotal, latestQty decimal.Decimal
		var countDistinctIDs, countUniqueQty uint64

		err := rows.Scan(&featureID, &meterID, &priceID, &sumTotal, &maxTotal, &countDistinctIDs, &countUniqueQty, &latestQty)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan usage result").
				Mark(ierr.ErrDatabase)
		}

		// Use composite key to avoid overwrites
		key := fmt.Sprintf("%s:%s:%s", featureID, meterID, priceID)
		results[key] = &events.UsageByCostSheetResult{
			CostSheetID:      costSheetID,
			FeatureID:        featureID,
			MeterID:          meterID,
			PriceID:          priceID,
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
	r.logger.Debugw("optimized costsheet usage query completed",
		"costsheet_id", costSheetID,
		"feature_count", len(results))

	return results, nil
}

// GetDetailedUsageAnalytics provides comprehensive usage analytics for costsheet with filtering, grouping, and time-series data
func (r *CostSheetUsageRepository) GetDetailedUsageAnalytics(ctx context.Context, costSheetID, externalCustomerID string, params *events.UsageAnalyticsParams, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo, sumBucketFeatures map[string]*events.SumBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	span := StartRepositorySpan(ctx, "costsheet_usage", "get_detailed_usage_analytics", map[string]interface{}{
		"costsheet_id":           costSheetID,
		"external_customer_id":   externalCustomerID,
		"feature_ids_count":      len(params.FeatureIDs),
		"property_filters_count": len(params.PropertyFilters),
	})
	defer FinishSpan(span)

	// Set default start/end times if not provided
	if params.EndTime.IsZero() {
		params.EndTime = time.Now().UTC()
	}

	if params.StartTime.IsZero() {
		// Default to last 30 days if not specified
		params.StartTime = params.EndTime.AddDate(0, 0, -30)
	}

	// Set default group by if not provided
	if len(params.GroupBy) == 0 {
		params.GroupBy = []string{"feature_id"}
	}

	// Validate group by values
	for _, groupBy := range params.GroupBy {
		if groupBy != "feature_id" && !strings.HasPrefix(groupBy, "properties.") {
			return nil, ierr.NewError("invalid group_by value").
				WithHint("Valid group_by values are 'feature_id' or 'properties.<field_name>' for costsheet").
				WithReportableDetails(map[string]interface{}{
					"group_by": params.GroupBy,
				}).
				Mark(ierr.ErrValidation)
		}
	}

	var allResults []*events.DetailedUsageAnalytic

	// Handle MAX with bucket features
	if len(maxBucketFeatures) > 0 {
		maxBucketResults, err := r.getMaxBucketAnalytics(ctx, costSheetID, externalCustomerID, params, maxBucketFeatures)
		if err != nil {
			SetSpanError(span, err)
			return nil, err
		}
		allResults = append(allResults, maxBucketResults...)
	}

	// Handle SUM with bucket features
	if len(sumBucketFeatures) > 0 {
		sumBucketResults, err := r.getSumBucketAnalytics(ctx, costSheetID, externalCustomerID, params, sumBucketFeatures)
		if err != nil {
			SetSpanError(span, err)
			return nil, err
		}
		allResults = append(allResults, sumBucketResults...)
	}

	// Handle other features (non-MAX and non-SUM with bucket)
	otherFeatureIDs := r.getOtherFeatureIDs(params.FeatureIDs, maxBucketFeatures, sumBucketFeatures)

	// Only process other features if we have some to process
	if len(otherFeatureIDs) > 0 || len(params.FeatureIDs) == 0 {
		otherParams := *params
		if len(otherFeatureIDs) > 0 {
			otherParams.FeatureIDs = otherFeatureIDs
		} else if len(params.FeatureIDs) == 0 {
			otherParams.FeatureIDs = []string{}
		}

		otherResults, err := r.getStandardAnalytics(ctx, costSheetID, externalCustomerID, &otherParams, maxBucketFeatures, sumBucketFeatures)
		if err != nil {
			SetSpanError(span, err)
			return nil, err
		}
		allResults = append(allResults, otherResults...)
	}

	SetSpanSuccess(span)
	return allResults, nil
}

// getOtherFeatureIDs returns feature IDs that are not MAX or SUM with bucket features
func (r *CostSheetUsageRepository) getOtherFeatureIDs(requestedFeatureIDs []string, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo, sumBucketFeatures map[string]*events.SumBucketFeatureInfo) []string {
	if len(requestedFeatureIDs) == 0 {
		return []string{}
	}

	otherFeatureIDs := make([]string, 0)
	for _, featureID := range requestedFeatureIDs {
		_, isMaxBucket := maxBucketFeatures[featureID]
		_, isSumBucket := sumBucketFeatures[featureID]
		if !isMaxBucket && !isSumBucket {
			otherFeatureIDs = append(otherFeatureIDs, featureID)
		}
	}
	return otherFeatureIDs
}

// getStandardAnalytics handles analytics for non-MAX and non-SUM with bucket features
func (r *CostSheetUsageRepository) getStandardAnalytics(ctx context.Context, costSheetID, externalCustomerID string, params *events.UsageAnalyticsParams, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo, sumBucketFeatures map[string]*events.SumBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	queryParams := []interface{}{
		params.TenantID,
		params.EnvironmentID,
		costSheetID,
		externalCustomerID,
		params.StartTime,
		params.EndTime,
	}

	// Add group by columns
	groupByColumns := []string{"feature_id", "price_id", "meter_id"}
	groupByColumnAliases := []string{"feature_id", "price_id", "meter_id"}
	groupByFieldMapping := make(map[string]string)
	groupByFieldMapping["feature_id"] = "feature_id"
	groupByFieldMapping["price_id"] = "price_id"
	groupByFieldMapping["meter_id"] = "meter_id"

	// Add requested grouping dimensions
	for _, groupBy := range params.GroupBy {
		if groupBy == "feature_id" {
			continue
		}
		if strings.HasPrefix(groupBy, "properties.") {
			propertyName := strings.TrimPrefix(groupBy, "properties.")
			if propertyName != "" {
				alias := "prop_" + strings.ReplaceAll(propertyName, ".", "_")
				sqlExpression := fmt.Sprintf("JSONExtractString(properties, '%s') AS %s", propertyName, alias)
				groupByColumns = append(groupByColumns, fmt.Sprintf("JSONExtractString(properties, '%s')", propertyName))
				groupByColumnAliases = append(groupByColumnAliases, sqlExpression)
				groupByFieldMapping[groupBy] = alias
			}
		}
	}

	// Build select columns
	selectColumns := []string{}
	if len(groupByColumnAliases) > 0 {
		selectColumns = append(selectColumns, strings.Join(groupByColumnAliases, ", "))
	}
	selectColumns = append(selectColumns,
		"SUM(qty_total * sign) AS total_usage",
		"MAX(qty_total * sign) AS max_usage",
		"argMax(qty_total, timestamp) AS latest_usage",
		"COUNT(DISTINCT unique_hash) AS count_unique_usage",
		"COUNT(DISTINCT id) AS event_count",
	)

	aggregateQuery := fmt.Sprintf(`
		SELECT 
			%s
		FROM costsheet_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND costsheet_id = ?
		AND external_customer_id = ?
		AND timestamp >= ?
		AND timestamp < ?
		AND sign != 0
	`, strings.Join(selectColumns, ",\n\t\t\t"))

	// Add filters
	filterParams := []interface{}{}

	// Feature IDs filter
	if len(params.FeatureIDs) > 0 {
		placeholders := make([]string, len(params.FeatureIDs))
		for i := range params.FeatureIDs {
			placeholders[i] = "?"
			filterParams = append(filterParams, params.FeatureIDs[i])
		}
		aggregateQuery += " AND feature_id IN (" + strings.Join(placeholders, ", ") + ")"
	}

	// Exclude MAX bucket features
	if len(maxBucketFeatures) > 0 {
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

	// Exclude SUM bucket features
	if len(sumBucketFeatures) > 0 {
		sumBucketFeatureIDs := make([]string, 0, len(sumBucketFeatures))
		for featureID := range sumBucketFeatures {
			sumBucketFeatureIDs = append(sumBucketFeatureIDs, featureID)
		}
		placeholders := make([]string, len(sumBucketFeatureIDs))
		for i := range sumBucketFeatureIDs {
			placeholders[i] = "?"
			filterParams = append(filterParams, sumBucketFeatureIDs[i])
		}
		aggregateQuery += " AND feature_id NOT IN (" + strings.Join(placeholders, ", ") + ")"
	}

	// Property filters
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
					for _, v := range values {
						filterParams = append(filterParams, v)
					}
				}
			}
		}
	}

	queryParams = append(queryParams, filterParams...)

	// Add GROUP BY clause
	aggregateQuery += fmt.Sprintf(" GROUP BY %s", strings.Join(groupByColumns, ", "))

	// Execute query
	rows, err := r.store.GetConn().Query(ctx, aggregateQuery, queryParams...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute costsheet standard analytics query").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	results := make([]*events.DetailedUsageAnalytic, 0)
	for rows.Next() {
		result := &events.DetailedUsageAnalytic{
			Properties: make(map[string]string),
		}

		// Prepare scan values
		scanValues := make([]interface{}, 0)
		scanValues = append(scanValues, &result.FeatureID, &result.PriceID, &result.MeterID)

		// Add property fields
		propertyValues := make(map[string]*string)
		for groupBy := range groupByFieldMapping {
			if strings.HasPrefix(groupBy, "properties.") {
				propertyName := strings.TrimPrefix(groupBy, "properties.")
				val := new(string)
				propertyValues[propertyName] = val
				scanValues = append(scanValues, val)
			}
		}

		// Add aggregation fields
		scanValues = append(scanValues, &result.TotalUsage, &result.MaxUsage, &result.LatestUsage, &result.CountUniqueUsage, &result.EventCount)

		if err := rows.Scan(scanValues...); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan costsheet analytics result").
				Mark(ierr.ErrDatabase)
		}

		// Populate properties
		for propertyName, val := range propertyValues {
			if val != nil && *val != "" {
				result.Properties[propertyName] = *val
			}
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Error iterating costsheet analytics results").
			Mark(ierr.ErrDatabase)
	}

	return results, nil
}

// getMaxBucketAnalytics handles analytics for MAX with bucket features
func (r *CostSheetUsageRepository) getMaxBucketAnalytics(ctx context.Context, costSheetID, externalCustomerID string, params *events.UsageAnalyticsParams, maxBucketFeatures map[string]*events.MaxBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
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
		totals, err := r.getMaxBucketTotals(ctx, costSheetID, externalCustomerID, &featureParams, featureInfo)
		if err != nil {
			return nil, err
		}

		// Get window-based time series points for each group
		if featureInfo.BucketSize != "" {
			// Need to get points per group to match totals
			for _, total := range totals {
				points, err := r.getMaxBucketPointsForGroup(ctx, costSheetID, externalCustomerID, &featureParams, featureInfo, total)
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

// getSumBucketAnalytics handles analytics for SUM with bucket features
func (r *CostSheetUsageRepository) getSumBucketAnalytics(ctx context.Context, costSheetID, externalCustomerID string, params *events.UsageAnalyticsParams, sumBucketFeatures map[string]*events.SumBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	// For SUM with bucket features, we need to:
	// 1. Calculate totals using bucket-based aggregation (meter's bucket size)
	// 2. Calculate time series points using request window size

	var allResults []*events.DetailedUsageAnalytic

	// Process each SUM with bucket feature separately since they may have different bucket sizes
	for featureID, featureInfo := range sumBucketFeatures {
		// Create a copy of params for this specific feature
		featureParams := *params
		featureParams.FeatureIDs = []string{featureID}

		// For now, return a simple implementation
		// TODO: Implement getSumBucketTotals and getSumBucketPointsForGroup similar to MAX bucket
		// This requires creating these methods following the same pattern as getMaxBucketTotals and getMaxBucketPointsForGroup

		// Temporary: Use standard analytics for SUM bucket features
		// This will be correct for totals but won't apply proper bucket logic
		r.logger.Warnw("SUM bucket feature detected but full bucket logic not yet implemented, using standard aggregation",
			"feature_id", featureID,
			"bucket_size", featureInfo.BucketSize,
		)

		results, err := r.getStandardAnalytics(ctx, costSheetID, externalCustomerID, &featureParams, make(map[string]*events.MaxBucketFeatureInfo), make(map[string]*events.SumBucketFeatureInfo))
		if err != nil {
			return nil, err
		}
		allResults = append(allResults, results...)
	}

	return allResults, nil
}

// getMaxBucketTotals calculates totals using bucket-based aggregation for MAX features
func (r *CostSheetUsageRepository) getMaxBucketTotals(ctx context.Context, costSheetID, externalCustomerID string, params *events.UsageAnalyticsParams, featureInfo *events.MaxBucketFeatureInfo) ([]*events.DetailedUsageAnalytic, error) {
	// Build bucket window expression
	bucketWindowExpr := r.formatWindowSize(featureInfo.BucketSize, nil)

	// Build group by columns
	groupByColumns := []string{"bucket_start", "feature_id", "price_id", "meter_id"}
	innerSelectColumns := []string{"feature_id", "price_id", "meter_id"}
	outerSelectColumns := []string{"feature_id", "price_id", "meter_id"}

	// Add grouping columns
	for _, groupBy := range params.GroupBy {
		if groupBy == "feature_id" {
			continue
		}
		if strings.HasPrefix(groupBy, "properties.") {
			propertyName := strings.TrimPrefix(groupBy, "properties.")
			groupByColumns = append(groupByColumns, fmt.Sprintf("JSONExtractString(properties, '%s')", propertyName))
			innerSelectColumns = append(innerSelectColumns, fmt.Sprintf("JSONExtractString(properties, '%s') as %s", propertyName, propertyName))
			outerSelectColumns = append(outerSelectColumns, propertyName)
		}
	}

	// Build inner query
	innerQuery := fmt.Sprintf(`
		SELECT
			%s as bucket_start,
			%s,
			max(qty_total * sign) as bucket_max,
			argMax(qty_total, timestamp) as bucket_latest,
			count(DISTINCT unique_hash) as bucket_count_unique,
			count(DISTINCT id) as event_count
		FROM costsheet_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND costsheet_id = ?
		AND external_customer_id = ?
		AND feature_id = ?
		AND timestamp >= ?
		AND timestamp < ?
		AND sign != 0`, bucketWindowExpr, strings.Join(innerSelectColumns, ", "))

	queryParams := []interface{}{
		params.TenantID,
		params.EnvironmentID,
		costSheetID,
		externalCustomerID,
		featureInfo.FeatureID,
		params.StartTime,
		params.EndTime,
	}

	// Add property filters
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
					for _, v := range values {
						queryParams = append(queryParams, v)
					}
				}
			}
		}
	}

	// Complete inner query
	innerQuery += fmt.Sprintf(" GROUP BY %s", strings.Join(groupByColumns, ", "))

	// Build complete query with CTE
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
		GROUP BY %s
	`, innerQuery, strings.Join(outerSelectColumns, ", "), strings.Join(outerSelectColumns, ", "))

	// Execute query
	rows, err := r.store.GetConn().Query(ctx, query, queryParams...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute costsheet max bucket analytics query").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	results := make([]*events.DetailedUsageAnalytic, 0)
	for rows.Next() {
		result := &events.DetailedUsageAnalytic{
			Properties: make(map[string]string),
			Points:     []events.UsageAnalyticPoint{},
		}

		// Prepare scan values
		scanValues := make([]interface{}, 0)
		scanValues = append(scanValues, &result.FeatureID, &result.PriceID, &result.MeterID)

		// Add property fields
		propertyValues := make(map[string]*string)
		for _, groupBy := range params.GroupBy {
			if strings.HasPrefix(groupBy, "properties.") {
				propertyName := strings.TrimPrefix(groupBy, "properties.")
				val := new(string)
				propertyValues[propertyName] = val
				scanValues = append(scanValues, val)
			}
		}

		// Add aggregation fields
		scanValues = append(scanValues, &result.TotalUsage, &result.MaxUsage, &result.LatestUsage, &result.CountUniqueUsage, &result.EventCount)

		if err := rows.Scan(scanValues...); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan costsheet max bucket analytics result").
				Mark(ierr.ErrDatabase)
		}

		// Populate properties
		for propertyName, val := range propertyValues {
			if val != nil && *val != "" {
				result.Properties[propertyName] = *val
			}
		}

		// Set metadata fields for MAX bucket features
		result.AggregationType = types.AggregationMax
		result.EventName = featureInfo.EventName

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Error iterating costsheet max bucket analytics results").
			Mark(ierr.ErrDatabase)
	}

	return results, nil
}

// getMaxBucketPointsForGroup calculates time series points for a specific group
func (r *CostSheetUsageRepository) getMaxBucketPointsForGroup(ctx context.Context, costSheetID, externalCustomerID string, params *events.UsageAnalyticsParams, featureInfo *events.MaxBucketFeatureInfo, group *events.DetailedUsageAnalytic) ([]events.UsageAnalyticPoint, error) {
	// Build window expression based on request window size
	// For costsheet, we use the bucket size as the window size if WindowSize is not specified
	windowSize := params.WindowSize
	if windowSize == "" {
		windowSize = featureInfo.BucketSize
	}
	windowExpr := r.formatWindowSize(windowSize, params.BillingAnchor)

	// For MAX with bucket features, we need to first get max within each bucket,
	// then aggregate those maxes within the request window
	// This version filters by the specific group's attributes (properties, etc.)

	// Build inner query with filters
	innerQuery := fmt.Sprintf(`
		SELECT
			%s as bucket_start,
			%s as window_start,
			max(qty_total * sign) as bucket_max,
			argMax(qty_total, timestamp) as bucket_latest,
			count(DISTINCT unique_hash) as bucket_count_unique,
			count(DISTINCT id) as event_count
		FROM costsheet_usage
		WHERE tenant_id = ?
		AND environment_id = ?
		AND costsheet_id = ?
		AND external_customer_id = ?
		AND feature_id = ?
		AND timestamp >= ?
		AND timestamp < ?
		AND sign != 0`, r.formatWindowSize(featureInfo.BucketSize, nil), windowExpr)

	queryParams := []interface{}{
		params.TenantID,
		params.EnvironmentID,
		costSheetID,
		externalCustomerID,
		featureInfo.FeatureID,
		params.StartTime,
		params.EndTime,
	}

	// Add filter for this specific group's price_id
	if group.PriceID != "" {
		innerQuery += " AND price_id = ?"
		queryParams = append(queryParams, group.PriceID)
	}

	// Add filter for this specific group's meter_id
	if group.MeterID != "" {
		innerQuery += " AND meter_id = ?"
		queryParams = append(queryParams, group.MeterID)
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
			WithHint("Failed to execute costsheet MAX bucket points query").
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
				WithHint("Failed to scan costsheet MAX bucket points row").
				Mark(ierr.ErrDatabase)
		}

		point.Timestamp = timestamp
		point.Cost = decimal.Zero // Will be calculated in enrichment
		points = append(points, point)
	}

	return points, nil
}

// formatWindowSize formats window size for ClickHouse queries
func (r *CostSheetUsageRepository) formatWindowSize(windowSize types.WindowSize, billingAnchor *time.Time) string {
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
