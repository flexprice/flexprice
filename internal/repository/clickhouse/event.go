package clickhouse

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/repository/clickhouse/builder"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type EventRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewEventRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) events.Repository {
	return &EventRepository{store: store, logger: logger}
}

func (r *EventRepository) InsertEvent(ctx context.Context, event *events.Event) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "event", "insert", map[string]interface{}{
		"event_id":   event.ID,
		"event_name": event.EventName,
	})
	defer FinishSpan(span)

	propertiesJSON, err := json.Marshal(event.Properties)
	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to marshal event properties").
			WithReportableDetails(map[string]interface{}{
				"event_id": event.ID,
			}).
			Mark(ierr.ErrValidation)
	}

	if err := event.Validate(); err != nil {
		SetSpanError(span, err)
		return err
	}

	query := `
		INSERT INTO events (
			id, external_customer_id, customer_id, tenant_id, event_name, timestamp, source, properties, environment_id
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?
		)
	`

	err = r.store.GetConn().Exec(ctx, query,
		event.ID,
		event.ExternalCustomerID,
		event.CustomerID,
		event.TenantID,
		event.EventName,
		event.Timestamp,
		event.Source,
		string(propertiesJSON),
		event.EnvironmentID,
	)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to insert event").
			WithReportableDetails(map[string]interface{}{
				"event_id":   event.ID,
				"event_name": event.EventName,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// BulkInsertEvents inserts multiple events in a bulk operation for better performance
func (r *EventRepository) BulkInsertEvents(ctx context.Context, events []*events.Event) error {
	if len(events) == 0 {
		return nil
	}

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "event", "bulk_insert", map[string]interface{}{
		"event_count": len(events),
	})
	defer FinishSpan(span)

	// split events in batches of 100
	eventsBatches := lo.Chunk(events, 100)

	for _, eventsBatch := range eventsBatches {
		// Prepare batch statement
		batch, err := r.store.GetConn().PrepareBatch(ctx, `
		INSERT INTO events (
			id, external_customer_id, customer_id, tenant_id, event_name, timestamp, source, properties, environment_id
		)
	`)
		if err != nil {
			SetSpanError(span, err)
			return ierr.WithError(err).
				WithHint("Failed to prepare batch for events").
				Mark(ierr.ErrDatabase)
		}

		// Validate all events before inserting
		for _, event := range eventsBatch {
			if err := event.Validate(); err != nil {
				SetSpanError(span, err)
				return err
			}

			propertiesJSON, err := json.Marshal(event.Properties)
			if err != nil {
				SetSpanError(span, err)
				return ierr.WithError(err).
					WithHint("Failed to marshal event properties").
					WithReportableDetails(map[string]interface{}{
						"event_id": event.ID,
					}).
					Mark(ierr.ErrValidation)
			}

			err = batch.Append(
				event.ID,
				event.ExternalCustomerID,
				event.CustomerID,
				event.TenantID,
				event.EventName,
				event.Timestamp,
				event.Source,
				string(propertiesJSON),
				event.EnvironmentID,
			)

			if err != nil {
				SetSpanError(span, err)
				return ierr.WithError(err).
					WithHint("Failed to append event to batch").
					WithReportableDetails(map[string]interface{}{
						"event_id":   event.ID,
						"event_name": event.EventName,
					}).
					Mark(ierr.ErrDatabase)
			}
		}

		// Execute the batch
		if err := batch.Send(); err != nil {
			SetSpanError(span, err)
			return ierr.WithError(err).
				WithHint("Failed to execute batch insert for events").
				WithReportableDetails(map[string]interface{}{
					"event_count": len(events),
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	SetSpanSuccess(span)
	return nil
}

type UsageResult struct {
	WindowSize time.Time
	Value      interface{}
}

func (r *EventRepository) GetUsage(ctx context.Context, params *events.UsageParams) (*events.AggregationResult, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "event", "get_usage", map[string]interface{}{
		"event_name":       params.EventName,
		"aggregation_type": params.AggregationType,
		"window_size":      params.WindowSize,
	})
	defer FinishSpan(span)

	// Validate multiplier if provided for aggregations that use it
	if params.Multiplier != nil {
		if *params.Multiplier <= 0 {
			err := ierr.NewError("invalid multiplier value").
				WithHint("Multiplier must be greater than zero").
				WithReportableDetails(map[string]interface{}{
					"multiplier": *params.Multiplier,
				}).
				Mark(ierr.ErrValidation)
			SetSpanError(span, err)
			return nil, err
		}

		// Only allow factor for supported aggregation types
		if params.AggregationType != types.AggregationSumWithMultiplier {
			err := ierr.NewError("multiplier not supported for this aggregation type").
				WithHint("Multiplier can only be used with SUM_WITH_MULTIPLIER aggregations").
				WithReportableDetails(map[string]interface{}{
					"aggregation_type": params.AggregationType,
				}).
				Mark(ierr.ErrValidation)
			SetSpanError(span, err)
			return nil, err
		}
	}

	aggregator := GetAggregator(params.AggregationType)
	if aggregator == nil {
		err := ierr.NewError("unsupported aggregation type").
			WithHint("The specified aggregation type is not supported").
			WithReportableDetails(map[string]interface{}{
				"aggregation_type": params.AggregationType,
			}).
			Mark(ierr.ErrValidation)
		SetSpanError(span, err)
		return nil, err
	}

	query := aggregator.GetQuery(ctx, params)
	log.Printf("Executing query: %s", query)

	rows, err := r.store.GetConn().Query(ctx, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to execute usage query").
			WithReportableDetails(map[string]interface{}{
				"event_name":       params.EventName,
				"aggregation_type": params.AggregationType,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var result events.AggregationResult
	result.Type = params.AggregationType
	result.EventName = params.EventName

	// For windowed queries, we need to process all rows
	if params.WindowSize != "" {
		for rows.Next() {
			var windowSize time.Time
			var value decimal.Decimal

			switch params.AggregationType {
			case types.AggregationCount, types.AggregationCountUnique:
				var countValue uint64
				if err := rows.Scan(&windowSize, &countValue); err != nil {
					SetSpanError(span, err)
					return nil, ierr.WithError(err).
						WithHint("Failed to scan count result").
						WithReportableDetails(map[string]interface{}{
							"window_size": windowSize,
							"count_value": countValue,
						}).
						Mark(ierr.ErrDatabase)
				}
				value = decimal.NewFromUint64(countValue)
			case types.AggregationSum, types.AggregationAvg, types.AggregationLatest, types.AggregationSumWithMultiplier:
				var floatValue float64
				if err := rows.Scan(&windowSize, &floatValue); err != nil {
					SetSpanError(span, err)
					return nil, ierr.WithError(err).
						WithHint("Failed to scan float result").
						WithReportableDetails(map[string]interface{}{
							"window_size": windowSize,
							"float_value": floatValue,
						}).
						Mark(ierr.ErrDatabase)
				}
				value = decimal.NewFromFloat(floatValue)
			default:
				err := ierr.NewError("unsupported aggregation type for scanning").
					WithHint("The specified aggregation type is not supported for scanning").
					WithReportableDetails(map[string]interface{}{
						"aggregation_type": params.AggregationType,
					}).
					Mark(ierr.ErrValidation)
				SetSpanError(span, err)
				return nil, err
			}

			result.Results = append(result.Results, events.UsageResult{
				WindowSize: windowSize,
				Value:      value,
			})
		}
	} else {
		if rows.Next() {
			switch params.AggregationType {
			case types.AggregationCount, types.AggregationCountUnique:
				var value uint64
				if err := rows.Scan(&value); err != nil {
					SetSpanError(span, err)
					return nil, ierr.WithError(err).
						WithHint("Failed to scan count result").
						WithReportableDetails(map[string]interface{}{
							"value": value,
						}).
						Mark(ierr.ErrDatabase)
				}
				result.Value = decimal.NewFromUint64(value)
			case types.AggregationSum, types.AggregationAvg, types.AggregationLatest, types.AggregationSumWithMultiplier:
				var value float64
				if err := rows.Scan(&value); err != nil {
					SetSpanError(span, err)
					return nil, ierr.WithError(err).
						WithHint("Failed to scan float result").
						WithReportableDetails(map[string]interface{}{
							"value": value,
						}).
						Mark(ierr.ErrDatabase)
				}
				result.Value = decimal.NewFromFloat(value)
			default:
				err := ierr.NewError("unsupported aggregation type for scanning").
					WithHint("The specified aggregation type is not supported for scanning").
					WithReportableDetails(map[string]interface{}{
						"aggregation_type": params.AggregationType,
					}).
					Mark(ierr.ErrValidation)
				SetSpanError(span, err)
				return nil, err
			}
		}
	}

	SetSpanSuccess(span)
	return &result, nil
}

func (r *EventRepository) GetUsageWithFilters(ctx context.Context, params *events.UsageWithFiltersParams) ([]*events.AggregationResult, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "event", "get_usage_with_filters", map[string]interface{}{
		"event_name":       params.UsageParams.EventName,
		"aggregation_type": params.AggregationType,
		"property_name":    params.PropertyName,
		"filter_groups":    len(params.FilterGroups),
	})
	defer FinishSpan(span)

	aggregator := GetAggregator(params.AggregationType)
	if aggregator == nil {
		err := ierr.NewError("unsupported aggregation type").
			WithHint("The specified aggregation type is not supported").
			WithReportableDetails(map[string]interface{}{
				"aggregation_type": params.AggregationType,
			}).
			Mark(ierr.ErrValidation)
		SetSpanError(span, err)
		return nil, err
	}

	// Build query using the new builder
	qb := builder.NewQueryBuilder().
		WithBaseFilters(ctx, params.UsageParams).
		WithAggregation(ctx, params.AggregationType, params.PropertyName).
		WithFilterGroups(ctx, params.FilterGroups)

	query, queryParams := qb.Build()

	r.logger.Debugw("executing filter groups query",
		"aggregation_type", params.AggregationType,
		"event_name", params.UsageParams.EventName,
		"filter_groups", len(params.FilterGroups),
		"query", query,
		"params", queryParams)

	rows, err := r.store.GetConn().Query(ctx, query, queryParams)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to execute usage query with filters").
			WithReportableDetails(map[string]interface{}{
				"event_name":       params.UsageParams.EventName,
				"aggregation_type": params.AggregationType,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var results []*events.AggregationResult
	for rows.Next() {
		var filterGroupID string

		result := &events.AggregationResult{}
		result.Type = params.AggregationType
		result.EventName = params.UsageParams.EventName

		// Use appropriate type based on aggregation
		switch params.AggregationType {
		case types.AggregationCount, types.AggregationCountUnique:
			var value uint64
			if err := rows.Scan(&filterGroupID, &value); err != nil {
				SetSpanError(span, err)
				return nil, ierr.WithError(err).
					WithHint("Failed to scan count row").
					WithReportableDetails(map[string]interface{}{
						"filter_group_id": filterGroupID,
					}).
					Mark(ierr.ErrDatabase)
			}
			result.Value = decimal.NewFromUint64(value)
		case types.AggregationSum, types.AggregationAvg, types.AggregationLatest, types.AggregationSumWithMultiplier:
			var value float64
			if err := rows.Scan(&filterGroupID, &value); err != nil {
				SetSpanError(span, err)
				return nil, ierr.WithError(err).
					WithHint("Failed to scan float row").
					WithReportableDetails(map[string]interface{}{
						"filter_group_id": filterGroupID,
					}).
					Mark(ierr.ErrDatabase)
			}
			result.Value = decimal.NewFromFloat(value)
		default:
			err := ierr.NewError("unsupported aggregation type").
				WithHint("The specified aggregation type is not supported").
				WithReportableDetails(map[string]interface{}{
					"aggregation_type": params.AggregationType,
				}).
				Mark(ierr.ErrValidation)
			SetSpanError(span, err)
			return nil, err
		}

		result.Metadata = map[string]string{
			"filter_group_id": filterGroupID,
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error iterating rows").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return results, nil
}

func (r *EventRepository) GetEvents(ctx context.Context, params *events.GetEventsParams) ([]*events.Event, uint64, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "event", "get_events", map[string]interface{}{
		"event_name":           params.EventName,
		"external_customer_id": params.ExternalCustomerID,
		"count_total":          params.CountTotal,
		"page_size":            params.PageSize,
	})
	defer FinishSpan(span)

	var totalCount uint64

	baseQuery := `
		SELECT 
			id,
			external_customer_id,
			customer_id,
			tenant_id,
			event_name,
			timestamp,
			source,
			properties,
			environment_id,
			ingested_at
		FROM events
		WHERE tenant_id = ?
	`
	args := make([]interface{}, 0)
	args = append(args, types.GetTenantID(ctx))

	// Add environment_id filter if present in context
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		baseQuery += " AND environment_id = ?"
		args = append(args, environmentID)
	}

	// Apply filters
	if params.EventID != "" {
		baseQuery += " AND id = ?"
		args = append(args, params.EventID)
	}
	if params.ExternalCustomerID != "" {
		baseQuery += " AND external_customer_id = ?"
		args = append(args, params.ExternalCustomerID)
	}
	if params.EventName != "" {
		baseQuery += " AND event_name = ?"
		args = append(args, params.EventName)
	}
	if !params.StartTime.IsZero() {
		baseQuery += " AND timestamp >= ?"
		args = append(args, params.StartTime)
	}
	if !params.EndTime.IsZero() {
		baseQuery += " AND timestamp <= ?"
		args = append(args, params.EndTime)
	}
	if params.Source != "" {
		baseQuery += " AND source = ?"
		args = append(args, params.Source)
	}

	// Apply property filters
	if len(params.PropertyFilters) > 0 {
		for property, values := range params.PropertyFilters {
			if len(values) > 0 {
				if len(values) == 1 {
					baseQuery += " AND JSONExtractString(properties, ?) = ?"
					args = append(args, property, values[0])
				} else {
					placeholders := make([]string, len(values))
					for i := range values {
						placeholders[i] = "?"
					}
					baseQuery += " AND JSONExtractString(properties, ?) IN (" + strings.Join(placeholders, ",") + ")"
					args = append(args, property)
					// Now append all values after the property
					for _, v := range values {
						args = append(args, v)
					}
				}
			}
		}
	}

	// Handle pagination and real-time refresh using composite keys
	if params.IterFirst != nil {
		baseQuery += " AND (timestamp, id) > (?, ?)"
		args = append(args, params.IterFirst.Timestamp, params.IterFirst.ID)
	} else if params.IterLast != nil {
		baseQuery += " AND (timestamp, id) < (?, ?)"
		args = append(args, params.IterLast.Timestamp, params.IterLast.ID)
	}

	// Count total if requested
	if params.CountTotal {
		countQuery := "SELECT COUNT(*) FROM (" + baseQuery + ") AS filtered_events"
		err := r.store.GetConn().QueryRow(ctx, countQuery, args...).Scan(&totalCount)
		if err != nil {
			SetSpanError(span, err)
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to count events").
				WithReportableDetails(map[string]interface{}{
					"event_name": params.EventName,
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	// Order by timestamp and ID by default
	if params.Sort == nil {
		params.Sort = lo.ToPtr("timestamp")
	}
	if params.Order == nil {
		params.Order = lo.ToPtr("DESC")
	}

	baseQuery += " ORDER BY " + strings.ToLower(*params.Sort) + " " + strings.ToUpper(*params.Order) + ", id DESC"

	// Apply limit and offset for pagination if using offset-based pagination
	if params.PageSize > 0 {
		baseQuery += " LIMIT ?"
		args = append(args, params.PageSize)

		if params.Offset > 0 {
			baseQuery += " OFFSET ?"
			args = append(args, params.Offset)
		}
	}

	r.logger.Debugw("executing get events query",
		"query", baseQuery,
		"args", args)

	// Execute query
	rows, err := r.store.GetConn().Query(ctx, baseQuery, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to query events").
			WithReportableDetails(map[string]interface{}{
				"event_name": params.EventName,
			}).
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var eventsList []*events.Event
	for rows.Next() {
		var event events.Event
		var propertiesJSON string

		err := rows.Scan(
			&event.ID,
			&event.ExternalCustomerID,
			&event.CustomerID,
			&event.TenantID,
			&event.EventName,
			&event.Timestamp,
			&event.Source,
			&propertiesJSON,
			&event.EnvironmentID,
			&event.IngestedAt,
		)
		if err != nil {
			SetSpanError(span, err)
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to scan event").
				WithReportableDetails(map[string]interface{}{
					"event_id": event.ID,
				}).
				Mark(ierr.ErrDatabase)
		}

		if err := json.Unmarshal([]byte(propertiesJSON), &event.Properties); err != nil {
			SetSpanError(span, err)
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to unmarshal event properties").
				WithReportableDetails(map[string]interface{}{
					"event_id":   event.ID,
					"properties": propertiesJSON,
				}).
				Mark(ierr.ErrValidation)
		}

		eventsList = append(eventsList, &event)
	}

	SetSpanSuccess(span)
	return eventsList, totalCount, nil
}

// FindUnprocessedEvents finds events that haven't been processed yet
// Uses keyset pagination for better performance with large datasets
func (r *EventRepository) FindUnprocessedEvents(ctx context.Context, params *events.FindUnprocessedEventsParams) ([]*events.Event, error) {
	span := StartRepositorySpan(ctx, "event", "find_unprocessed_events", map[string]interface{}{
		"batch_size":           params.BatchSize,
		"external_customer_id": params.ExternalCustomerID,
	})
	defer FinishSpan(span)

	// Use ANTI JOIN for better performance with ClickHouse
	// This avoids the need for subqueries in the WHERE clause
	// Also using the primary key ORDER BY for efficiency
	query := `
		SELECT 
			e.id, e.external_customer_id, e.customer_id, e.tenant_id, 
			e.event_name, e.timestamp, e.source, e.properties, 
			e.environment_id, e.ingested_at
		FROM events e
		ANTI JOIN (
			SELECT id, tenant_id, environment_id
			FROM events_processed
			WHERE tenant_id = ?
			AND environment_id = ?
		) AS p
		ON e.id = p.id AND e.tenant_id = p.tenant_id AND e.environment_id = p.environment_id
		WHERE e.tenant_id = ?
		AND e.environment_id = ?
	`

	args := []interface{}{
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
		types.GetTenantID(ctx),
		types.GetEnvironmentID(ctx),
	}

	// Add the last seen ID and timestamp for keyset pagination if provided
	if params.LastID != "" && !params.LastTimestamp.IsZero() {
		// Use keyset pagination for better performance
		query += " AND (e.timestamp, e.id) < (?, ?)"
		args = append(args, params.LastTimestamp, params.LastID)
	}

	// Add filters if provided
	if params.ExternalCustomerID != "" {
		query += " AND e.external_customer_id = ?"
		args = append(args, params.ExternalCustomerID)
	}

	if params.EventName != "" {
		query += " AND e.event_name = ?"
		args = append(args, params.EventName)
	}

	if !params.StartTime.IsZero() {
		query += " AND e.timestamp >= ?"
		args = append(args, params.StartTime)
	}

	if !params.EndTime.IsZero() {
		query += " AND e.timestamp <= ?"
		args = append(args, params.EndTime)
	}

	// Add sorting for consistent keyset pagination
	// Using the same fields we're filtering on for the keyset
	query += " ORDER BY e.timestamp DESC, e.id DESC"

	// Add batch size limit
	if params.BatchSize > 0 {
		query += " LIMIT ?"
		args = append(args, params.BatchSize)
	} else {
		// Default to a reasonable batch size to avoid huge result sets
		query += " LIMIT 100"
	}

	r.logger.Debugw("executing find unprocessed events query",
		"query", query,
		"external_customer_id", params.ExternalCustomerID,
		"event_name", params.EventName,
		"batch_size", params.BatchSize,
	)

	// Execute the query
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query unprocessed events").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var eventsList []*events.Event
	for rows.Next() {
		var event events.Event
		var propertiesJSON string

		err := rows.Scan(
			&event.ID,
			&event.ExternalCustomerID,
			&event.CustomerID,
			&event.TenantID,
			&event.EventName,
			&event.Timestamp,
			&event.Source,
			&propertiesJSON,
			&event.EnvironmentID,
			&event.IngestedAt,
		)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan event").
				Mark(ierr.ErrDatabase)
		}

		if err := json.Unmarshal([]byte(propertiesJSON), &event.Properties); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to unmarshal event properties").
				Mark(ierr.ErrValidation)
		}

		eventsList = append(eventsList, &event)
	}

	SetSpanSuccess(span)
	return eventsList, nil
}
