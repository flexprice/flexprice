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
	"github.com/flexprice/flexprice/internal/types"
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
			var value interface{}

			switch params.AggregationType {
			case types.AggregationCount:
				var countValue uint64
				if err := rows.Scan(&windowSize, &countValue); err != nil {
					return nil, fmt.Errorf("scan result: %w", err)
				}
				value = float64(countValue)
			case types.AggregationSum, types.AggregationAvg:
				var floatValue float64
				if err := rows.Scan(&windowSize, &floatValue); err != nil {
					return nil, fmt.Errorf("scan result: %w", err)
				}
				value = floatValue
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
				result.Value = float64(value)
			case types.AggregationSum, types.AggregationAvg:
				var value float64
				if err := rows.Scan(&value); err != nil {
					return nil, fmt.Errorf("scan result: %w", err)
				}
				result.Value = value
			default:
				return nil, fmt.Errorf("unsupported aggregation type for scanning: %s", params.AggregationType)
			}
		}
	}

	return &result, nil
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
