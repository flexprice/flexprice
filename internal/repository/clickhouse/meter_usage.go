package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// MeterUsageRepository implements events.MeterUsageRepository using ClickHouse.
// Query logic is delegated to MeterUsageAggregator (strategy pattern) and
// MeterUsageQueryBuilder (SQL construction), keeping this file focused on
// I/O: batch inserts, query execution, and row scanning.
type MeterUsageRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
	qb     *MeterUsageQueryBuilder
}

func NewMeterUsageRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) events.MeterUsageRepository {
	return &MeterUsageRepository{
		store:  store,
		logger: logger,
		qb:     NewMeterUsageQueryBuilder(),
	}
}

// BulkInsertMeterUsage inserts meter usage records in batches of 100
func (r *MeterUsageRepository) BulkInsertMeterUsage(ctx context.Context, records []*events.MeterUsage) error {
	if len(records) == 0 {
		return nil
	}

	batches := lo.Chunk(records, 100)

	for _, batch := range batches {
		stmt, err := r.store.GetConn().PrepareBatch(ctx, `
			INSERT INTO meter_usage (
				id, tenant_id, environment_id, external_customer_id, meter_id, event_name,
				timestamp, qty_total, unique_hash, source, properties
			)
		`)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to prepare batch for meter_usage insert").
				Mark(ierr.ErrDatabase)
		}

		for _, record := range batch {
			propsStr := r.marshalProperties(record)

			err = stmt.Append(
				record.ID,
				record.TenantID,
				record.EnvironmentID,
				record.ExternalCustomerID,
				record.MeterID,
				record.EventName,
				record.Timestamp,
				record.QtyTotal,
				record.UniqueHash,
				record.Source,
				propsStr,
			)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to append row to meter_usage batch").
					WithReportableDetails(map[string]interface{}{"event_id": record.ID}).
					Mark(ierr.ErrDatabase)
			}
		}

		if err := stmt.Send(); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to send meter_usage batch").
				Mark(ierr.ErrDatabase)
		}
	}

	return nil
}

// IsDuplicate checks if a meter usage record with the given unique_hash already exists
func (r *MeterUsageRepository) IsDuplicate(ctx context.Context, meterID, uniqueHash string) (bool, error) {
	query := `
		SELECT 1
		FROM meter_usage
		WHERE meter_id = ?
		AND unique_hash = ?
		LIMIT 1
	`

	var exists int
	err := r.store.GetConn().QueryRow(ctx, query, meterID, uniqueHash).Scan(&exists)
	if err != nil {
		// If no rows, it means no duplicate
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, ierr.WithError(err).
			WithHint("Failed to check for duplicate meter usage event").
			Mark(ierr.ErrDatabase)
	}

	return exists == 1, nil
}

// GetUsage queries aggregated usage for a single meter using the aggregator strategy
func (r *MeterUsageRepository) GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	aggregator := GetMeterUsageAggregator(params.AggregationType)
	query := aggregator.GetQuery(ctx, params, r.qb)
	_, args := r.qb.BuildWhereClause(params)

	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	if windowExpr != "" {
		return r.executeWindowedQuery(ctx, query, args, params)
	}

	return r.executeScalarQuery(ctx, query, args, params)
}

// GetUsageMultiMeter queries aggregated usage for multiple meters, grouped by meter_id
func (r *MeterUsageRepository) GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	if params == nil || len(params.MeterIDs) == 0 {
		return nil, ierr.NewError("params with meter_ids are required").Mark(ierr.ErrValidation)
	}

	aggregator := GetMeterUsageAggregator(params.AggregationType)
	// Extract aggregation expressions from the aggregator type
	aggExpr, countExpr := getMeterUsageAggExprs(aggregator)
	query := BuildMultiMeterQuery(aggExpr, countExpr, params, r.qb)
	_, args := r.qb.BuildWhereClause(params)

	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor)
	if windowExpr != "" {
		return r.executeMultiMeterWindowedQuery(ctx, query, args, params)
	}

	return r.executeMultiMeterScalarQuery(ctx, query, args, params)
}

// --- Private execution helpers ---

func (r *MeterUsageRepository) executeWindowedQuery(ctx context.Context, query string, args []interface{}, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query meter_usage with window").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	result := &events.MeterUsageAggregationResult{
		MeterID:         params.MeterID,
		AggregationType: params.AggregationType,
		TotalValue:      decimal.Zero,
		Points:          make([]events.MeterUsageResult, 0),
	}

	for rows.Next() {
		var point events.MeterUsageResult
		if err := rows.Scan(&point.WindowStart, &point.Value, &point.EventCount); err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to scan meter_usage window row").Mark(ierr.ErrDatabase)
		}
		result.TotalValue = result.TotalValue.Add(point.Value)
		result.EventCount += point.EventCount
		result.Points = append(result.Points, point)
	}

	return result, nil
}

func (r *MeterUsageRepository) executeScalarQuery(ctx context.Context, query string, args []interface{}, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	var value decimal.Decimal
	var eventCount uint64

	err := r.store.GetConn().QueryRow(ctx, query, args...).Scan(&value, &eventCount)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query meter_usage").Mark(ierr.ErrDatabase)
	}

	return &events.MeterUsageAggregationResult{
		MeterID:         params.MeterID,
		AggregationType: params.AggregationType,
		TotalValue:      value,
		EventCount:      eventCount,
	}, nil
}

func (r *MeterUsageRepository) executeMultiMeterWindowedQuery(ctx context.Context, query string, args []interface{}, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query meter_usage multi-meter with window").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	meterResults := make(map[string]*events.MeterUsageAggregationResult)
	for rows.Next() {
		var meterID string
		var point events.MeterUsageResult
		if err := rows.Scan(&meterID, &point.WindowStart, &point.Value, &point.EventCount); err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to scan meter_usage multi-meter window row").Mark(ierr.ErrDatabase)
		}

		res, ok := meterResults[meterID]
		if !ok {
			res = &events.MeterUsageAggregationResult{
				MeterID:         meterID,
				AggregationType: params.AggregationType,
				TotalValue:      decimal.Zero,
				Points:          make([]events.MeterUsageResult, 0),
			}
			meterResults[meterID] = res
		}
		res.TotalValue = res.TotalValue.Add(point.Value)
		res.EventCount += point.EventCount
		res.Points = append(res.Points, point)
	}

	results := make([]*events.MeterUsageAggregationResult, 0, len(meterResults))
	for _, res := range meterResults {
		results = append(results, res)
	}
	return results, nil
}

func (r *MeterUsageRepository) executeMultiMeterScalarQuery(ctx context.Context, query string, args []interface{}, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to query meter_usage multi-meter").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	results := make([]*events.MeterUsageAggregationResult, 0)
	for rows.Next() {
		var meterID string
		var value decimal.Decimal
		var eventCount uint64
		if err := rows.Scan(&meterID, &value, &eventCount); err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to scan meter_usage multi-meter row").Mark(ierr.ErrDatabase)
		}
		results = append(results, &events.MeterUsageAggregationResult{
			MeterID:         meterID,
			AggregationType: params.AggregationType,
			TotalValue:      value,
			EventCount:      eventCount,
		})
	}

	return results, nil
}

// marshalProperties serializes event properties to a JSON string for ClickHouse
func (r *MeterUsageRepository) marshalProperties(record *events.MeterUsage) string {
	if record.Properties == nil {
		return ""
	}
	propsJSON, err := json.Marshal(record.Properties)
	if err != nil {
		r.logger.Errorw("failed to marshal properties for meter_usage",
			"event_id", record.ID,
			"error", err,
		)
		return ""
	}
	return string(propsJSON)
}

// GetUsageForBucketedMeters returns windowed aggregation results for bucketed meters.
// Returns *events.AggregationResult (shared type with feature_usage) for compatibility
// with calculateBucketedMeterCost and windowed commitment logic in billing.go.
func (r *MeterUsageRepository) GetUsageForBucketedMeters(ctx context.Context, params *events.MeterUsageQueryParams) (*events.AggregationResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	query, args := r.qb.BuildBucketedQuery(params)

	r.logger.Debugw("executing bucketed meter usage query",
		"meter_id", params.MeterID,
		"window_size", params.WindowSize,
		"group_by", params.GroupByProperty,
	)

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute bucketed meter usage query").
			WithReportableDetails(map[string]interface{}{
				"meter_id":    params.MeterID,
				"window_size": params.WindowSize,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var result events.AggregationResult
	result.Type = params.AggregationType
	result.MeterID = params.MeterID

	hasGroupBy := params.GroupByProperty != "" && validMeterUsageGroupByPattern.MatchString(params.GroupByProperty)

	for rows.Next() {
		var total decimal.Decimal
		var windowStart time.Time
		var value decimal.Decimal

		if hasGroupBy {
			var groupKey string
			if err := rows.Scan(&total, &windowStart, &value, &groupKey); err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to scan bucketed meter usage row (with group_key)").
					Mark(ierr.ErrDatabase)
			}
			result.Value = total
			result.Results = append(result.Results, events.UsageResult{
				WindowSize: windowStart,
				Value:      value,
				GroupKey:   groupKey,
			})
		} else {
			if err := rows.Scan(&total, &windowStart, &value); err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to scan bucketed meter usage row").
					Mark(ierr.ErrDatabase)
			}
			result.Value = total
			result.Results = append(result.Results, events.UsageResult{
				WindowSize: windowStart,
				Value:      value,
			})
		}
	}

	return &result, nil
}

// GetDistinctMeterIDs returns the set of meter_ids that have data in meter_usage
// for the given customer(s) and time range.
func (r *MeterUsageRepository) GetDistinctMeterIDs(ctx context.Context, params *events.MeterUsageQueryParams) ([]string, error) {
	if params == nil {
		return nil, nil
	}

	where, args := r.qb.BuildWhereClause(params)
	finalClause, settings := r.qb.BuildFinalClause(params.UseFinal)

	query := fmt.Sprintf(`
		SELECT DISTINCT meter_id
		FROM meter_usage %s
		WHERE %s
		ORDER BY meter_id
		%s
	`, finalClause, where, settings)

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query distinct meter_ids from meter_usage").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var meterIDs []string
	for rows.Next() {
		var meterID string
		if err := rows.Scan(&meterID); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan distinct meter_id").
				Mark(ierr.ErrDatabase)
		}
		meterIDs = append(meterIDs, meterID)
	}

	return meterIDs, nil
}

// GetDetailedAnalytics provides comprehensive analytics with filtering, grouping, and time-series data.
func (r *MeterUsageRepository) GetDetailedAnalytics(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]*events.MeterUsageDetailedResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	// Parse and validate group-by columns
	groupByResult, err := r.qb.BuildDetailedGroupByColumns(params)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid group_by configuration").
			Mark(ierr.ErrValidation)
	}

	// Check if source is in group_by
	sourceInGroupBy := false
	for _, g := range params.GroupBy {
		if g == "source" {
			sourceInGroupBy = true
			break
		}
	}

	// Build SELECT columns: group-by aliases + aggregation columns
	selectColumns := make([]string, 0, len(groupByResult.Aliases)+6)
	if len(groupByResult.Aliases) > 0 {
		selectColumns = append(selectColumns, groupByResult.Aliases...)
	}
	aggColumns := buildConditionalAggregationColumns(params.AggregationTypes)
	selectColumns = append(selectColumns, aggColumns...)
	if !sourceInGroupBy {
		selectColumns = append(selectColumns, "groupUniqArray(source) AS sources")
	}

	// Build WHERE clause
	where, args := r.qb.BuildDetailedWhereClause(params)
	finalClause, settings := r.qb.BuildFinalClause(params.UseFinal)

	query := fmt.Sprintf(`
		SELECT
			%s
		FROM meter_usage %s
		WHERE %s
	`, joinSelect(selectColumns), finalClause, where)

	if len(groupByResult.Columns) > 0 {
		query += " GROUP BY " + joinSelect(groupByResult.Columns)
	}

	if settings != "" {
		query += "\n" + settings
	}

	r.logger.Debugw("executing detailed meter usage analytics query",
		"query", query,
		"group_by", params.GroupBy,
		"property_filters", params.PropertyFilters,
	)

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute detailed meter usage analytics query").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []*events.MeterUsageDetailedResult

	for rows.Next() {
		result := &events.MeterUsageDetailedResult{
			Properties: make(map[string]string),
			Points:     make([]events.MeterUsageDetailedPoint, 0),
		}

		// Build scan targets: group-by columns + 5 aggregation values + optional sources
		totalGroupByCols := len(groupByResult.Columns)
		expectedCols := totalGroupByCols + 5 // total_usage, max_usage, latest_usage, count_unique_usage, event_count
		if !sourceInGroupBy {
			expectedCols++ // sources array
		}

		scanArgs := make([]interface{}, expectedCols)
		groupByTargets := make([]string, totalGroupByCols)
		for i := range groupByTargets {
			scanArgs[i] = &groupByTargets[i]
		}

		scanArgs[totalGroupByCols] = &result.TotalUsage
		scanArgs[totalGroupByCols+1] = &result.MaxUsage
		scanArgs[totalGroupByCols+2] = &result.LatestUsage
		scanArgs[totalGroupByCols+3] = &result.CountUniqueUsage
		scanArgs[totalGroupByCols+4] = &result.EventCount
		if !sourceInGroupBy {
			result.Sources = []string{}
			scanArgs[totalGroupByCols+5] = &result.Sources
		}

		if err := rows.Scan(scanArgs...); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan detailed meter usage analytics row").
				Mark(ierr.ErrDatabase)
		}

		// Map scanned group-by values to result fields
		for i, col := range groupByResult.Columns {
			value := groupByTargets[i]
			switch col {
			case "meter_id":
				result.MeterID = value
			case "source":
				result.Source = value
			default:
				// Property group-by: extract property name from JSONExtractString expression
				if strings.HasPrefix(col, "JSONExtractString(properties, '") {
					start := len("JSONExtractString(properties, '")
					end := strings.Index(col[start:], "'")
					if end > 0 {
						propName := col[start : start+end]
						result.Properties[propName] = value
					}
				}
			}
		}

		// Fetch time-series points if window_size is specified
		if params.WindowSize != "" {
			points, err := r.getDetailedAnalyticsPoints(ctx, params, result, groupByResult)
			if err != nil {
				return nil, err
			}
			result.Points = points
		}

		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Error iterating detailed meter usage analytics rows").
			Mark(ierr.ErrDatabase)
	}

	return results, nil
}

// getDetailedAnalyticsPoints fetches time-series breakdown for a single group result.
func (r *MeterUsageRepository) getDetailedAnalyticsPoints(
	ctx context.Context,
	params *events.MeterUsageDetailedAnalyticsParams,
	result *events.MeterUsageDetailedResult,
	groupByResult *DetailedGroupByResult,
) ([]events.MeterUsageDetailedPoint, error) {
	query, args := r.qb.BuildDetailedPointsQuery(params, result, groupByResult)
	if query == "" {
		return nil, nil
	}

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query detailed meter usage analytics points").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var points []events.MeterUsageDetailedPoint
	for rows.Next() {
		var p events.MeterUsageDetailedPoint
		if err := rows.Scan(&p.WindowStart, &p.TotalUsage, &p.MaxUsage, &p.LatestUsage, &p.CountUniqueUsage, &p.EventCount); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan detailed meter usage analytics point").
				Mark(ierr.ErrDatabase)
		}
		points = append(points, p)
	}

	return points, nil
}

// joinSelect joins select columns with comma+newline for readability
func joinSelect(cols []string) string {
	return strings.Join(cols, ",\n\t\t\t")
}

// getMeterUsageAggExprs returns the SQL expression pair for a given aggregator.
// This bridges the aggregator strategy with multi-meter queries that need raw expressions.
func getMeterUsageAggExprs(agg MeterUsageAggregator) (aggExpr string, countExpr string) {
	countExpr = "COUNT(DISTINCT id)"

	switch agg.(type) {
	case *MeterUsageSumAggregator:
		aggExpr = "SUM(qty_total)"
	case *MeterUsageCountAggregator:
		aggExpr = "COUNT(DISTINCT id)"
	case *MeterUsageCountUniqueAggregator:
		aggExpr = "COUNT(DISTINCT unique_hash)"
	case *MeterUsageMaxAggregator:
		aggExpr = "MAX(qty_total)"
	case *MeterUsageAvgAggregator:
		aggExpr = "AVG(qty_total)"
	case *MeterUsageLatestAggregator:
		aggExpr = fmt.Sprintf("argMax(qty_total, timestamp)")
	default:
		aggExpr = "SUM(qty_total)"
	}

	return aggExpr, countExpr
}
