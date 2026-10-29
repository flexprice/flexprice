package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
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

// insertTimeout caps each batch insert; without it the driver uses the caller's much longer deadline.
const insertTimeout = 30 * time.Second

// groupKeySeparator joins a row's group-by values into one map key; NUL never occurs in a value.
const groupKeySeparator = "\x00"

// propertyKeysSampleRows is the number of latest rows sampled per meter for property keys.
const propertyKeysSampleRows = 10000

// BulkInsertMeterUsage inserts meter usage records in batches of 100
func (r *MeterUsageRepository) BulkInsertMeterUsage(ctx context.Context, records []*events.MeterUsage) error {
	span := StartRepositorySpan(ctx, "meter_usage", "bulk_insert", map[string]interface{}{
		"record_count": len(records),
	})
	defer FinishSpan(span)

	if len(records) == 0 {
		SetSpanSuccess(span)
		return nil
	}

	batches := lo.Chunk(records, 100)

	for _, batch := range batches {
		batchCtx, cancel := context.WithTimeout(ctx, insertTimeout)
		defer cancel()

		stmt, err := r.store.GetConn().PrepareBatch(batchCtx, `
			INSERT INTO meter_usage (
				id, tenant_id, environment_id, external_customer_id, meter_id, event_name,
				timestamp, qty_total, unique_hash, source, properties, ingested_at
			)
		`)
		if err != nil {
			SetSpanError(span, err)
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
				record.IngestedAt,
			)
			if err != nil {
				SetSpanError(span, err)
				return ierr.WithError(err).
					WithHint("Failed to append row to meter_usage batch").
					WithReportableDetails(map[string]interface{}{"event_id": record.ID}).
					Mark(ierr.ErrDatabase)
			}
		}

		if err := stmt.Send(); err != nil {
			SetSpanError(span, err)
			return ierr.WithError(err).
				WithHint("Failed to send meter_usage batch").
				Mark(ierr.ErrDatabase)
		}
	}

	SetSpanSuccess(span)
	return nil
}

// IsDuplicate checks if a meter usage record with the given unique_hash already exists
func (r *MeterUsageRepository) IsDuplicate(ctx context.Context, meterID, uniqueHash string) (bool, error) {
	span := StartRepositorySpan(ctx, "meter_usage", "is_duplicate", map[string]interface{}{
		"meter_id": meterID,
	})
	defer FinishSpan(span)

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
			SetSpanSuccess(span)
			return false, nil
		}
		SetSpanError(span, err)
		return false, ierr.WithError(err).
			WithHint("Failed to check for duplicate meter usage event").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return exists == 1, nil
}

// GetUsage queries aggregated usage for a single meter using the aggregator strategy
func (r *MeterUsageRepository) GetUsage(ctx context.Context, params *events.MeterUsageQueryParams) (*events.MeterUsageAggregationResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_usage", map[string]interface{}{
		"meter_id":         params.MeterID,
		"aggregation_type": params.AggregationType,
		"window_size":      params.WindowSize,
	})
	defer FinishSpan(span)

	aggregator := GetMeterUsageAggregator(params.AggregationType)
	query := aggregator.GetQuery(ctx, params, r.qb)
	_, args := r.qb.BuildWhereClause(params)

	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
	var (
		result *events.MeterUsageAggregationResult
		err    error
	)
	if windowExpr != "" {
		result, err = r.executeWindowedQuery(ctx, query, args, params)
	} else {
		result, err = r.executeScalarQuery(ctx, query, args, params)
	}
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	SetSpanSuccess(span)
	return result, nil
}

// GetEarliestUsageTimestamp returns the earliest event timestamp matching the
// params' filters within [StartTime, EndTime), or nil when no events match.
// No FINAL: ReplacingMergeTree duplicates share the timestamp, so min() is unaffected.
func (r *MeterUsageRepository) GetEarliestUsageTimestamp(ctx context.Context, params *events.MeterUsageQueryParams) (*time.Time, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_earliest_usage_timestamp", map[string]interface{}{
		"meter_id":             params.MeterID,
		"external_customer_id": params.ExternalCustomerID,
	})
	defer FinishSpan(span)

	where, args := r.qb.BuildWhereClause(params)
	query := fmt.Sprintf(`
		SELECT min(timestamp) AS earliest, count() AS event_count
		FROM meter_usage
		WHERE %s
		SETTINGS max_memory_usage = 96636764160
	`, where)

	var earliest time.Time
	var eventCount uint64
	if err := r.store.GetConn().QueryRow(ctx, query, args...).Scan(&earliest, &eventCount); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query earliest meter usage timestamp").
			Mark(ierr.ErrDatabase)
	}
	if eventCount == 0 {
		SetSpanSuccess(span)
		return nil, nil
	}
	earliest = earliest.UTC()
	SetSpanSuccess(span)
	return &earliest, nil
}

// GetUsageMultiMeter queries aggregated usage for multiple meters, grouped by meter_id
func (r *MeterUsageRepository) GetUsageMultiMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageAggregationResult, error) {
	if params == nil || len(params.MeterIDs) == 0 {
		return nil, ierr.NewError("params with meter_ids are required").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_usage_multi_meter", map[string]interface{}{
		"meter_ids_count":  len(params.MeterIDs),
		"aggregation_type": params.AggregationType,
		"window_size":      params.WindowSize,
	})
	defer FinishSpan(span)

	aggregator := GetMeterUsageAggregator(params.AggregationType)
	// Extract aggregation expressions from the aggregator type
	aggExpr, countExpr := getMeterUsageAggExprs(aggregator)
	query := BuildMultiMeterQuery(aggExpr, countExpr, params, r.qb)
	_, args := r.qb.BuildWhereClause(params)

	windowExpr := formatWindowSizeWithBillingAnchor(params.WindowSize, params.BillingAnchor, params.Timezone)
	var (
		results []*events.MeterUsageAggregationResult
		err     error
	)
	if windowExpr != "" {
		results, err = r.executeMultiMeterWindowedQuery(ctx, query, args, params)
	} else {
		results, err = r.executeMultiMeterScalarQuery(ctx, query, args, params)
	}
	if err != nil {
		SetSpanError(span, err)
		return nil, err
	}
	SetSpanSuccess(span)
	return results, nil
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
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).WithHint("Error iterating meter_usage multi-meter window rows").Mark(ierr.ErrDatabase)
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
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).WithHint("Error iterating meter_usage multi-meter rows").Mark(ierr.ErrDatabase)
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
		r.logger.Error(context.Background(), "failed to marshal properties for meter_usage",
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

	span := StartRepositorySpan(ctx, "meter_usage", "get_usage_for_bucketed_meters", map[string]interface{}{
		"meter_id":    params.MeterID,
		"window_size": params.WindowSize,
		"group_by":    params.GroupBy,
	})
	defer FinishSpan(span)

	query, args := r.qb.BuildBucketedQuery(params)

	r.logger.Debug(ctx, "executing bucketed meter usage query",
		"meter_id", params.MeterID,
		"window_size", params.WindowSize,
		"group_by", params.GroupBy,
	)

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
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

	// Both no-group and grouped SQL paths emit the same 5 columns: total,
	// total_event_count, timestamp, value, event_count. Per-row dim values
	// are no longer surfaced — no consumer reads them.
	for rows.Next() {
		var total decimal.Decimal
		var totalEventCount uint64
		var windowStart time.Time
		var value decimal.Decimal
		var eventCount uint64

		if err := rows.Scan(&total, &totalEventCount, &windowStart, &value, &eventCount); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan bucketed meter usage row").
				Mark(ierr.ErrDatabase)
		}
		result.Value = total
		result.EventCount = totalEventCount
		result.Results = append(result.Results, events.UsageResult{
			WindowSize: windowStart,
			Value:      value,
			EventCount: eventCount,
		})
	}
	// rows.Next() returns false on both end-of-rows and iteration error.
	// Without this check we'd silently return partial results as success and
	// downstream billing totals would be wrong.
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating bucketed meter usage rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return &result, nil
}

// GetSourcesForBucketedMeter returns distinct source values for a bucketed meter using
// the same WHERE conditions as GetUsageForBucketedMeters. Called by the analytics service
// layer when expand:"source" is requested, keeping MeterUsageQueryParams free of analytics concerns.
func (r *MeterUsageRepository) GetSourcesForBucketedMeter(ctx context.Context, params *events.MeterUsageQueryParams) ([]string, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_sources_for_bucketed_meter", map[string]interface{}{
		"meter_id": params.MeterID,
	})
	defer FinishSpan(span)

	where, args := r.qb.BuildWhereClause(params)
	query := fmt.Sprintf("SELECT groupUniqArray(source) FROM meter_usage WHERE %s", where)

	var sources []string
	if err := r.store.GetConn().QueryRow(ctx, query, args...).Scan(&sources); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to collect distinct sources for bucketed meter").
			WithReportableDetails(map[string]interface{}{"meter_id": params.MeterID}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return sources, nil
}

// GetUsageForBucketedMetersDetailed runs the analytics-side bucketed query:
// 1) the aggregate query returns one row per (source, properties) combo with
// per-combo TotalUsage / EventCount; 2) for each combo, a follow-up query
// fetches per-bucket Points. Matches feature_usage's two-query pattern so the
// service layer doesn't have to re-group rows in Go.
//
// Callers MUST set params.GroupBy; otherwise an empty slice is returned (no
// combos to enumerate). For the no-grouping case use GetUsageForBucketedMeters.
func (r *MeterUsageRepository) GetUsageForBucketedMetersDetailed(ctx context.Context, params *events.MeterUsageQueryParams) ([]*events.MeterUsageDetailedResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_usage_for_bucketed_meters_detailed", map[string]interface{}{
		"meter_id":    params.MeterID,
		"window_size": params.WindowSize,
		"group_by":    params.GroupBy,
	})
	defer FinishSpan(span)

	aggQuery, aggArgs, dims := r.qb.BuildBucketedAggregateQuery(params)
	if aggQuery == "" {
		SetSpanSuccess(span)
		return nil, nil
	}

	r.logger.Debug(ctx, "executing bucketed detailed aggregate query",
		"meter_id", params.MeterID,
		"window_size", params.WindowSize,
		"group_by", params.GroupBy,
	)

	rows, err := r.store.GetConn().Query(ctx, aggQuery, aggArgs...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to execute bucketed detailed aggregate query").
			WithReportableDetails(map[string]interface{}{
				"meter_id":    params.MeterID,
				"window_size": params.WindowSize,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	// Each row carries (dim values..., group_total, group_event_count).
	results := make([]*events.MeterUsageDetailedResult, 0)
	for rows.Next() {
		dimVals := make([]string, len(dims))
		var groupTotal decimal.Decimal
		var groupEC uint64

		scanArgs := make([]interface{}, 0, len(dims)+2)
		for i := range dimVals {
			scanArgs = append(scanArgs, &dimVals[i])
		}
		scanArgs = append(scanArgs, &groupTotal, &groupEC)

		if err := rows.Scan(scanArgs...); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan bucketed detailed aggregate row").
				Mark(ierr.ErrDatabase)
		}

		res := &events.MeterUsageDetailedResult{
			MeterID:    params.MeterID,
			Properties: make(map[string]string),
			TotalUsage: groupTotal,
			EventCount: groupEC,
		}
		// MAX bucketed meters also surface MaxUsage = TotalUsage for response parity.
		if params.AggregationType != types.AggregationSum {
			res.MaxUsage = groupTotal
		}
		for i, d := range dims {
			if d.IsSource() {
				res.Source = dimVals[i]
			} else if d.IsProperty() && dimVals[i] != "" {
				// Drop missing-key dims (JSONExtractString returns "" when the key isn't present)
				res.Properties[d.PropertyName] = dimVals[i]
			}
		}
		results = append(results, res)
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating bucketed detailed aggregate rows").
			Mark(ierr.ErrDatabase)
	}

	// Fetch Points per combo (one query each) when the caller asked for a
	// window_size. Skip the round-trips when no time-series is needed.
	if params.WindowSize != "" {
		for _, res := range results {
			pts, err := r.fetchBucketedComboPoints(ctx, params, dims, res.Source, res.Properties, params.AggregationType)
			if err != nil {
				SetSpanError(span, err)
				return nil, err
			}
			res.Points = pts
		}
	}

	SetSpanSuccess(span)
	return results, nil
}

// fetchBucketedComboPoints runs BuildBucketedPointsQuery for one combo and
// converts the bucket rows into the MeterUsageDetailedPoint shape the response
// builder consumes.
func (r *MeterUsageRepository) fetchBucketedComboPoints(
	ctx context.Context,
	params *events.MeterUsageQueryParams,
	dims []BucketedGroupByDim,
	comboSource string,
	comboProps map[string]string,
	aggType types.AggregationType,
) ([]events.MeterUsageDetailedPoint, error) {
	query, args := r.qb.BuildBucketedPointsQuery(params, dims, comboSource, comboProps)
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to execute bucketed combo points query").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	pts := make([]events.MeterUsageDetailedPoint, 0)
	for rows.Next() {
		var bucketStart time.Time
		var bucketValue decimal.Decimal
		var bucketEC uint64
		if err := rows.Scan(&bucketStart, &bucketValue, &bucketEC); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan bucketed combo points row").
				Mark(ierr.ErrDatabase)
		}
		p := events.MeterUsageDetailedPoint{
			WindowStart: bucketStart,
			TotalUsage:  bucketValue,
			EventCount:  bucketEC,
		}
		if aggType != types.AggregationSum {
			p.MaxUsage = bucketValue
		}
		pts = append(pts, p)
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Error iterating bucketed combo points rows").
			Mark(ierr.ErrDatabase)
	}
	return pts, nil
}

// GetDistinctMeterIDs returns the set of meter_ids that have data in meter_usage
// for the given customer(s) and time range.
func (r *MeterUsageRepository) GetDistinctMeterIDs(ctx context.Context, params *events.MeterUsageQueryParams) ([]string, error) {
	if params == nil {
		return nil, nil
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_distinct_meter_ids", map[string]interface{}{
		"external_customer_id":  params.ExternalCustomerID,
		"external_customer_ids": len(params.ExternalCustomerIDs),
	})
	defer FinishSpan(span)

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
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query distinct meter_ids from meter_usage").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var meterIDs []string
	for rows.Next() {
		var meterID string
		if err := rows.Scan(&meterID); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan distinct meter_id").
				Mark(ierr.ErrDatabase)
		}
		meterIDs = append(meterIDs, meterID)
	}

	SetSpanSuccess(span)
	return meterIDs, nil
}

// GetDetailedAnalytics provides comprehensive analytics with filtering, grouping, and time-series data.
func (r *MeterUsageRepository) GetDetailedAnalytics(ctx context.Context, params *events.MeterUsageDetailedAnalyticsParams) ([]*events.MeterUsageDetailedResult, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_detailed_analytics", map[string]interface{}{
		"external_customer_id":   params.ExternalCustomerID,
		"meter_ids_count":        len(params.MeterIDs),
		"group_by":               params.GroupBy,
		"property_filters_count": len(params.PropertyFilters),
		"window_size":            params.WindowSize,
	})
	defer FinishSpan(span)

	// Parse and validate group-by columns
	groupByResult, err := r.qb.BuildDetailedGroupByColumns(params)
	if err != nil {
		SetSpanError(span, err)
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
	aggColumns := buildMeterUsageAggregationColumns(params.AggregationTypes)
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

	r.logger.Debug(ctx, "executing detailed meter usage analytics query",
		"query", query,
		"group_by", params.GroupBy,
		"property_filters", params.PropertyFilters,
	)

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to execute detailed meter usage analytics query").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []*events.MeterUsageDetailedResult
	byGroup := make(map[string]*events.MeterUsageDetailedResult)

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
			SetSpanError(span, err)
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
			case "external_customer_id":
				result.ExternalCustomerID = value
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

		byGroup[strings.Join(groupByTargets, groupKeySeparator)] = result
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating detailed meter usage analytics rows").
			Mark(ierr.ErrDatabase)
	}

	if params.WindowSize != "" {
		if err := r.getDetailedAnalyticsPoints(ctx, params, groupByResult, byGroup); err != nil {
			SetSpanError(span, err)
			return nil, err
		}
	}

	SetSpanSuccess(span)
	return results, nil
}

// getDetailedAnalyticsPoints fetches the time-series points of every group in one query
// and appends each point to its group in byGroup.
func (r *MeterUsageRepository) getDetailedAnalyticsPoints(
	ctx context.Context,
	params *events.MeterUsageDetailedAnalyticsParams,
	groupByResult *DetailedGroupByResult,
	byGroup map[string]*events.MeterUsageDetailedResult,
) error {
	query, args := r.qb.BuildDetailedPointsQuery(params, groupByResult)
	if query == "" {
		return nil
	}

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to query detailed meter usage analytics points").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var p events.MeterUsageDetailedPoint
	groupValues := make([]string, len(groupByResult.Columns))
	scanArgs := make([]interface{}, 0, len(groupValues)+6)
	for i := range groupValues {
		scanArgs = append(scanArgs, &groupValues[i])
	}
	scanArgs = append(scanArgs, &p.WindowStart, &p.TotalUsage, &p.MaxUsage, &p.LatestUsage, &p.CountUniqueUsage, &p.EventCount)

	for rows.Next() {
		if err := rows.Scan(scanArgs...); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to scan detailed meter usage analytics point").
				Mark(ierr.ErrDatabase)
		}
		if result, ok := byGroup[strings.Join(groupValues, groupKeySeparator)]; ok {
			result.Points = append(result.Points, p)
		}
	}

	return rows.Err()
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
		aggExpr = "ifNotFinite(AVG(qty_total), 0)"
	case *MeterUsageLatestAggregator:
		aggExpr = "argMax(qty_total, timestamp)"
	default:
		aggExpr = "SUM(qty_total)"
	}

	return aggExpr, countExpr
}

// buildMeterUsageAggregationColumns builds SQL aggregation columns for meter_usage
// analytics queries.
//
// Unlike feature_usage's buildConditionalAggregationColumns (where total_usage
// only holds a real value when SUM is in the aggregation set), here total_usage
// always holds the PRIMARY aggregation result regardless of type — COUNT meters
// get total_usage = COUNT(DISTINCT id), MAX meters get total_usage = MAX(qty_total),
// etc. Priority order matches frequency (SUM → COUNT → COUNT_UNIQUE → MAX → AVG → LATEST).
//
// This keeps the Go-side simple: r.TotalUsage and p.TotalUsage carry the
// aggregation-aware value with no further routing needed. The per-type columns
// (max_usage, latest_usage, count_unique_usage) remain so multi-aggregation
// queries still get all values in a single round-trip; for single-aggregation
// queries (the common case) total_usage and the matching per-type column will
// hold the same value, which is harmless.
func buildMeterUsageAggregationColumns(aggTypes []types.AggregationType) []string {
	aggSet := make(map[types.AggregationType]bool, len(aggTypes))
	for _, aggType := range aggTypes {
		aggSet[aggType] = true
	}

	var primaryExpr string
	switch {
	case aggSet[types.AggregationSum]:
		primaryExpr = "SUM(qty_total)"
	case aggSet[types.AggregationCount]:
		primaryExpr = "COUNT(DISTINCT id)"
	case aggSet[types.AggregationCountUnique]:
		primaryExpr = "COUNT(DISTINCT unique_hash)"
	case aggSet[types.AggregationMax]:
		primaryExpr = "MAX(qty_total)"
	case aggSet[types.AggregationAvg]:
		primaryExpr = "ifNotFinite(AVG(qty_total), 0)"
	case aggSet[types.AggregationLatest]:
		primaryExpr = "argMax(qty_total, timestamp)"
	default:
		primaryExpr = "toDecimal128(0, 9)"
	}

	columns := []string{primaryExpr + " AS total_usage"}

	if aggSet[types.AggregationMax] {
		columns = append(columns, "MAX(qty_total) AS max_usage")
	} else {
		columns = append(columns, "toDecimal128(0, 9) AS max_usage")
	}

	if aggSet[types.AggregationLatest] {
		columns = append(columns, "argMax(qty_total, timestamp) AS latest_usage")
	} else {
		columns = append(columns, "toDecimal128(0, 9) AS latest_usage")
	}

	if aggSet[types.AggregationCountUnique] {
		columns = append(columns, "COUNT(DISTINCT unique_hash) AS count_unique_usage")
	} else {
		columns = append(columns, "toUInt64(0) AS count_unique_usage")
	}

	// event_count is the total distinct event count for every query.
	columns = append(columns, "COUNT(DISTINCT id) AS event_count")

	return columns
}

// GetMeterUsageForExport retrieves meter usage data for export in batches.
func (r *MeterUsageRepository) GetMeterUsageForExport(ctx context.Context, startTime, endTime time.Time, batchSize int, offset int) ([]*events.MeterUsage, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "meter_usage", "get_meter_usage_for_export", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
		"batch_size":     batchSize,
		"offset":         offset,
		"start_time":     startTime,
		"end_time":       endTime,
	})
	defer FinishSpan(span)

	query := `
		SELECT
			id,
			tenant_id,
			environment_id,
			external_customer_id,
			event_name,
			source,
			timestamp,
			ingested_at,
			properties,
			meter_id,
			qty_total,
			unique_hash
		FROM meter_usage FINAL
		WHERE tenant_id = ?
		  AND environment_id = ?
		  AND timestamp >= ?
		  AND timestamp < ?
		ORDER BY timestamp DESC
		LIMIT ? OFFSET ?
		SETTINGS max_memory_usage = 96636764160
	`

	rows, err := r.store.GetConn().Query(ctx, query, tenantID, environmentID, startTime, endTime, batchSize, offset)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query meter_usage for export in batch").
			WithReportableDetails(map[string]interface{}{
				"batch_size": batchSize,
				"offset":     offset,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []*events.MeterUsage
	for rows.Next() {
		var usage events.MeterUsage
		var propertiesJSON string

		if err := rows.Scan(
			&usage.ID,
			&usage.TenantID,
			&usage.EnvironmentID,
			&usage.ExternalCustomerID,
			&usage.EventName,
			&usage.Source,
			&usage.Timestamp,
			&usage.IngestedAt,
			&propertiesJSON,
			&usage.MeterID,
			&usage.QtyTotal,
			&usage.UniqueHash,
		); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan meter_usage row").
				Mark(ierr.ErrDatabase)
		}

		if propertiesJSON != "" {
			if err := json.Unmarshal([]byte(propertiesJSON), &usage.Properties); err != nil {
				r.logger.Info(ctx, "failed to parse properties JSON",
					"event_id", usage.ID,
					"error", err)
				usage.Properties = make(map[string]interface{})
			}
		}

		results = append(results, &usage)
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating meter_usage rows").
			Mark(ierr.ErrDatabase)
	}

	r.logger.Debug(ctx, "meter_usage export batch query completed",
		"tenant_id", tenantID,
		"environment_id", environmentID,
		"batch_size", batchSize,
		"offset", offset,
		"records_in_batch", len(results))

	SetSpanSuccess(span)
	return results, nil
}

// CountMeterUsageForExport returns an approximate row count for the export window.
// Uses count() without FINAL so the gate stays cheap; duplicates may inflate the
// count slightly, which is acceptable for a fail-fast volume check.
func (r *MeterUsageRepository) CountMeterUsageForExport(ctx context.Context, startTime, endTime time.Time) (int, error) {
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	span := StartRepositorySpan(ctx, "meter_usage", "count_meter_usage_for_export", map[string]interface{}{
		"tenant_id":      tenantID,
		"environment_id": environmentID,
		"start_time":     startTime,
		"end_time":       endTime,
	})
	defer FinishSpan(span)

	query := `
		SELECT count()
		FROM meter_usage
		WHERE tenant_id = ?
		  AND environment_id = ?
		  AND timestamp >= ?
		  AND timestamp < ?
		SETTINGS max_memory_usage = 96636764160
	`

	var count uint64
	if err := r.store.GetConn().QueryRow(ctx, query, tenantID, environmentID, startTime, endTime).Scan(&count); err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count meter_usage rows for export").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return int(count), nil
}

// GetDailyUsageByMeter returns per-day SUM(qty_total) over
// [StartTime, EndTime) for every meter in MeterIDs, keyed by meter id. The
// running total is deliberately left to the caller: accumulating here would
// pin the result to one window start, which is what forced a query per line
// item.
func (r *MeterUsageRepository) GetDailyUsageByMeter(ctx context.Context, params *events.DailyUsageParams) (map[string][]events.DailyUsagePoint, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}
	if len(params.MeterIDs) == 0 {
		return map[string][]events.DailyUsagePoint{}, nil
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_daily_usage_by_meter", map[string]interface{}{
		"meter_count": len(params.MeterIDs),
	})
	defer FinishSpan(span)

	query, args := r.qb.BuildDailyUsageQuery(params)

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query daily usage").
			WithReportableDetails(map[string]interface{}{"meter_count": len(params.MeterIDs)}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	byMeter := make(map[string][]events.DailyUsagePoint, len(params.MeterIDs))
	for rows.Next() {
		var meterID string
		var d time.Time
		var dayQty decimal.Decimal
		if err := rows.Scan(&meterID, &d, &dayQty); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan daily usage row").
				Mark(ierr.ErrDatabase)
		}
		byMeter[meterID] = append(byMeter[meterID], events.DailyUsagePoint{Day: d, Qty: dayQty})
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating daily usage rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return byMeter, nil
}

// GetUsageActivitySince returns the distinct customers with usage ingested
// after params.IngestedAfter. Filtering on ingested_at rather than timestamp is
// what makes backdated events count as activity for the run that receives them.
func (r *MeterUsageRepository) GetUsageActivitySince(ctx context.Context, params *events.UsageActivityParams) (*events.UsageActivity, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_usage_activity_since", map[string]interface{}{
		"tenant_id":      params.TenantID,
		"environment_id": params.EnvironmentID,
	})
	defer FinishSpan(span)

	query, args := r.qb.BuildUsageActivityQuery(params)

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query usage activity").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	activity := &events.UsageActivity{}
	for rows.Next() {
		var externalCustomerID string
		if err := rows.Scan(&externalCustomerID); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan usage activity row").
				Mark(ierr.ErrDatabase)
		}
		if externalCustomerID == "" {
			activity.Unattributed = true
			continue
		}
		activity.ExternalCustomerIDs = append(activity.ExternalCustomerIDs, externalCustomerID)
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating usage activity rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return activity, nil
}

// GetEventsByEventIDAndExternalCustomerID returns the customer's meter_usage rows for a single event
// within window, one per meter it matched, or none if not yet processed.
func (r *MeterUsageRepository) GetEventsByEventIDAndExternalCustomerID(ctx context.Context, tenantID, environmentID, externalCustomerID, eventID string, window events.TimeRange) ([]*events.MeterUsage, error) {
	span := StartRepositorySpan(ctx, "meter_usage", "get_events_by_event_id_and_external_customer_id", map[string]interface{}{
		"tenant_id":            tenantID,
		"environment_id":       environmentID,
		"external_customer_id": externalCustomerID,
		"event_id":             eventID,
		"start_time":           window.Start,
		"end_time":             window.End,
	})
	defer FinishSpan(span)

	// LIMIT 1 BY meter_id keeps the latest row per meter while duplicates are still unmerged.
	query := `
		SELECT
			id,
			tenant_id,
			environment_id,
			external_customer_id,
			event_name,
			source,
			timestamp,
			ingested_at,
			properties,
			meter_id,
			qty_total,
			unique_hash
		FROM meter_usage
		WHERE tenant_id = ?
		  AND environment_id = ?
		  AND external_customer_id = ?
		  AND id = ?
		  AND timestamp >= ?
		  AND timestamp < ?
		ORDER BY meter_id, ingested_at DESC
		LIMIT 1 BY meter_id
		SETTINGS max_memory_usage = 96636764160
	`

	rows, err := r.store.GetConn().Query(ctx, query, tenantID, environmentID, externalCustomerID, eventID, window.Start, window.End)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query meter_usage by event ID").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var out []*events.MeterUsage
	for rows.Next() {
		var usage events.MeterUsage
		var propertiesJSON string
		if err := rows.Scan(
			&usage.ID,
			&usage.TenantID,
			&usage.EnvironmentID,
			&usage.ExternalCustomerID,
			&usage.EventName,
			&usage.Source,
			&usage.Timestamp,
			&usage.IngestedAt,
			&propertiesJSON,
			&usage.MeterID,
			&usage.QtyTotal,
			&usage.UniqueHash,
		); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan meter_usage row").
				Mark(ierr.ErrDatabase)
		}
		if propertiesJSON != "" {
			if err := json.Unmarshal([]byte(propertiesJSON), &usage.Properties); err != nil {
				r.logger.Error(ctx, "failed to parse properties JSON", "event_id", usage.ID, "error", err)
				usage.Properties = make(map[string]interface{})
			}
		}
		out = append(out, &usage)
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating meter_usage rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return out, nil
}

// GetPropertyKeys samples the latest rows of each meter and returns the keys by merged count, highest first.
func (r *MeterUsageRepository) GetPropertyKeys(ctx context.Context, params *events.PropertyLookupParams) ([]string, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}
	if len(params.MeterIDs) == 0 {
		return nil, nil
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_property_keys", map[string]interface{}{
		"external_customer_id": params.ExternalCustomerID,
		"meter_count":          len(params.MeterIDs),
	})
	defer FinishSpan(span)

	// Without a customer, the sort key doesn't order rows by timestamp, so any rows are sampled instead of the latest.
	customerScope, order := "", ""
	args := []interface{}{params.TenantID, params.EnvironmentID}
	if params.ExternalCustomerID != "" {
		customerScope, order = "AND external_customer_id = ?", "ORDER BY timestamp DESC"
		args = append(args, params.ExternalCustomerID)
	}
	query := fmt.Sprintf(`
		SELECT key, count() AS seen
		FROM (
			SELECT properties FROM meter_usage
			WHERE tenant_id = ? AND environment_id = ? %s
				AND meter_id = ? AND timestamp >= ? AND timestamp < ?
			%s
			LIMIT %d
		)
		ARRAY JOIN JSONExtractKeys(properties) AS key
		GROUP BY key
		SETTINGS max_memory_usage = 96636764160
	`, customerScope, order, propertyKeysSampleRows)

	counts := make(map[string]uint64)
	for _, meterID := range params.MeterIDs {
		rows, err := r.store.GetConn().Query(ctx, query, append(args, meterID, params.StartTime, params.EndTime)...)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to query property keys").
				Mark(ierr.ErrDatabase)
		}
		for rows.Next() {
			var key string
			var seen uint64
			if err := rows.Scan(&key, &seen); err != nil {
				rows.Close()
				SetSpanError(span, err)
				return nil, ierr.WithError(err).
					WithHint("Failed to scan property key row").
					Mark(ierr.ErrDatabase)
			}
			counts[key] += seen
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Error iterating property key rows").
				Mark(ierr.ErrDatabase)
		}
	}

	keys := lo.Keys(counts)
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})

	SetSpanSuccess(span)
	return keys, nil
}

// GetPropertyValues ranks the key's values by row count; no FINAL since the count only orders them.
func (r *MeterUsageRepository) GetPropertyValues(ctx context.Context, params *events.PropertyLookupParams) ([]string, error) {
	if params == nil {
		return nil, ierr.NewError("params are required").Mark(ierr.ErrValidation)
	}
	if len(params.MeterIDs) == 0 {
		return nil, nil
	}

	span := StartRepositorySpan(ctx, "meter_usage", "get_property_values", map[string]interface{}{
		"external_customer_id": params.ExternalCustomerID,
		"meter_count":          len(params.MeterIDs),
		"key":                  params.Key,
	})
	defer FinishSpan(span)

	where, whereArgs := r.qb.BuildWhereClause(&events.MeterUsageQueryParams{
		TenantID:           params.TenantID,
		EnvironmentID:      params.EnvironmentID,
		ExternalCustomerID: params.ExternalCustomerID,
		MeterIDs:           params.MeterIDs,
		StartTime:          params.StartTime,
		EndTime:            params.EndTime,
	})
	query := fmt.Sprintf(`
		SELECT JSONExtractString(properties, ?) AS value
		FROM meter_usage
		WHERE %s AND value != '' AND startsWith(value, ?)
		GROUP BY value
		ORDER BY count() DESC, value
		LIMIT ?
		SETTINGS max_memory_usage = 96636764160
	`, where)
	args := append([]interface{}{params.Key}, whereArgs...)
	args = append(args, params.Prefix, params.Limit)

	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query property values").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan property value row").
				Mark(ierr.ErrDatabase)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating property value rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return values, nil
}
