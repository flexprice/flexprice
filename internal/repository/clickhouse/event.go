package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/repository/clickhouse/builder"
	"github.com/flexprice/flexprice/internal/types"
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
	propertiesJSON, err := json.Marshal(event.Properties)
	if err != nil {
		return fmt.Errorf("marshal properties: %w", err)
	}

	// Adding a layer to validate the event before inserting it
	if err := event.Validate(); err != nil {
		return fmt.Errorf("validate event: %w", err)
	}

	query := `
		INSERT INTO events (
			id, external_customer_id, customer_id, tenant_id, event_name, timestamp, source, properties
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?
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
	)

	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}

	return nil
}

type UsageResult struct {
	WindowSize time.Time
	Value      interface{}
}

func (r *EventRepository) GetUsage(ctx context.Context, params *events.UsageParams) (*events.AggregationResult, error) {
	aggregator := GetAggregator(params.AggregationType)
	if aggregator == nil {
		return nil, fmt.Errorf("unsupported aggregation type: %s", params.AggregationType)
	}

	query := aggregator.GetQuery(ctx, params)
	log.Printf("Executing query: %s", query)

	rows, err := r.store.GetConn().Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("execute query: %w", err)
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
			case types.AggregationCount:
				var countValue uint64
				if err := rows.Scan(&windowSize, &countValue); err != nil {
					return nil, fmt.Errorf("scan result: %w", err)
				}
				value = decimal.NewFromUint64(countValue)
			case types.AggregationSum, types.AggregationAvg:
				var floatValue float64
				if err := rows.Scan(&windowSize, &floatValue); err != nil {
					return nil, fmt.Errorf("scan result: %w", err)
				}
				value = decimal.NewFromFloat(floatValue)
			default:
				return nil, fmt.Errorf("unsupported aggregation type for scanning: %s", params.AggregationType)
			}

			result.Results = append(result.Results, events.UsageResult{
				WindowSize: windowSize,
				Value:      value,
			})
		}
	} else {
		// Non-windowed query - process single row
		if rows.Next() {
			switch params.AggregationType {
			case types.AggregationCount:
				var value uint64
				if err := rows.Scan(&value); err != nil {
					return nil, fmt.Errorf("scan result: %w", err)
				}
				result.Value = decimal.NewFromUint64(value)
			case types.AggregationSum, types.AggregationAvg:
				var value float64
				if err := rows.Scan(&value); err != nil {
					return nil, fmt.Errorf("scan result: %w", err)
				}
				result.Value = decimal.NewFromFloat(value)
			default:
				return nil, fmt.Errorf("unsupported aggregation type for scanning: %s", params.AggregationType)
			}
		}
	}

	return &result, nil
}

func (r *EventRepository) GetUsageWithFilters(ctx context.Context, params *events.UsageWithFiltersParams) ([]*events.AggregationResult, error) {
	if params == nil || params.UsageParams == nil {
		return nil, fmt.Errorf("params cannot be nil")
	}

	// Build query using the new builder
	qb := builder.NewQueryBuilder().
		WithBaseFilters(ctx, params.UsageParams).
		WithAggregation(ctx, params.AggregationType, params.PropertyName).
		WithFilterGroups(ctx, params.FilterGroups)

	query, queryParams := qb.Build()

	r.logger.Debugw("executing filter groups query",
		"event_name", params.EventName,
		"filter_groups", len(params.FilterGroups),
		"query", query,
		"params", queryParams)

	// Execute query with named parameters
	rows, err := r.store.GetConn().Query(ctx, query, queryParams)
	if err != nil {
		return nil, fmt.Errorf("failed to execute query: %w", err)
	}
	defer rows.Close()

	// Process results
	var results []*events.AggregationResult
	for rows.Next() {
		var filterGroupID string

		result := &events.AggregationResult{}
		result.Type = params.AggregationType
		result.EventName = params.UsageParams.EventName

		// Use appropriate type based on aggregation
		switch params.AggregationType {
		case types.AggregationCount:
			var value uint64
			if err := rows.Scan(&filterGroupID, &value); err != nil {
				return nil, fmt.Errorf("failed to scan count row: %w", err)
			}
			result.Value = decimal.NewFromUint64(value)
		case types.AggregationSum, types.AggregationAvg:
			var value float64
			if err := rows.Scan(&filterGroupID, &value); err != nil {
				return nil, fmt.Errorf("failed to scan float row: %w", err)
			}
			result.Value = decimal.NewFromFloat(value)
		default:
			return nil, fmt.Errorf("unsupported aggregation type: %v", params.AggregationType)
		}

		result.Metadata = map[string]string{
			"filter_group_id": filterGroupID,
		}
		results = append(results, result)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	return results, nil
}

func (r *EventRepository) GetEvents(ctx context.Context, params *events.GetEventsParams) ([]*events.Event, error) {
	baseQuery := `
		SELECT 
			id,
			external_customer_id,
			customer_id,
			tenant_id,
			event_name,
			timestamp,
			source,
			properties
		FROM events
		WHERE tenant_id = ?
	`
	args := make([]interface{}, 0)
	args = append(args, types.GetTenantID(ctx))

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

	// Handle pagination and real-time refresh using composite keys
	if params.IterFirst != nil {
		baseQuery += " AND (timestamp, id) > (?, ?)"
		args = append(args, params.IterFirst.Timestamp, params.IterFirst.ID)
	} else if params.IterLast != nil {
		baseQuery += " AND (timestamp, id) < (?, ?)"
		args = append(args, params.IterLast.Timestamp, params.IterLast.ID)
	}

	// Order by timestamp and ID
	baseQuery += " ORDER BY timestamp DESC, id DESC"
	baseQuery += " LIMIT ?"
	args = append(args, params.PageSize+1)

	// Execute query
	rows, err := r.store.GetConn().Query(ctx, baseQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
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
		)
		if err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		if err := json.Unmarshal([]byte(propertiesJSON), &event.Properties); err != nil {
			return nil, fmt.Errorf("unmarshal properties: %w", err)
		}

		eventsList = append(eventsList, &event)
	}

	return eventsList, nil
}
