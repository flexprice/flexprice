package clickhouse

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	"github.com/flexprice/flexprice/internal/types"
)

type EventRepository struct {
	store *clickhouse.ClickHouseStore
}

func NewEventRepository(store *clickhouse.ClickHouseStore) events.Repository {
	return &EventRepository{store: store}
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
	query := aggregator.GetQuery(ctx, params)

	var results []events.UsageResult // Use the domain-level UsageResult struct
	var err error

	if params.WindowSize != "" {
		// If WindowSize is provided, fetch multiple rows
		rows, err := r.store.GetConn().Query(ctx, query)
		if err != nil {
			return nil, fmt.Errorf("query rows: %w", err)
		}
		defer rows.Close()

		for rows.Next() {
			var windowSize time.Time
			var value interface{}

			switch aggregator.GetType() {
			case types.AggregationCount:
				var count uint64
				err = rows.Scan(&windowSize, &count)
				value = count
			default:
				var num float64
				err = rows.Scan(&windowSize, &num)
				value = num
			}

			if err != nil {
				return nil, fmt.Errorf("scan row: %w", err)
			}

			results = append(results, events.UsageResult{ // Use domain-level UsageResult struct
				WindowSize: windowSize,
				Value:      value,
			})
		}

		if rows.Err() != nil {
			return nil, fmt.Errorf("rows error: %w", rows.Err())
		}

	} else {
		// If WindowSize is not provided, fetch a single value
		var value interface{}
		switch aggregator.GetType() {
		case types.AggregationCount:
			var count uint64
			err = r.store.GetConn().QueryRow(ctx, query).Scan(&count)
			value = count
		default:
			var num float64
			err = r.store.GetConn().QueryRow(ctx, query).Scan(&num)
			value = num
		}

		if err != nil {
			return nil, fmt.Errorf("get usage: %w", err)
		}

		results = append(results, events.UsageResult{
			Value: value,
		})
	}

	return &events.AggregationResult{
		Results:   results,
		EventName: params.EventName,
		Type:      aggregator.GetType(),
	}, nil
}
