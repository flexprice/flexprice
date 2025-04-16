package clickhouse

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type ProcessedEventRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewProcessedEventRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) events.ProcessedEventRepository {
	return &ProcessedEventRepository{store: store, logger: logger}
}

// InsertProcessedEvent inserts a single processed event
func (r *ProcessedEventRepository) InsertProcessedEvent(ctx context.Context, event *events.ProcessedEvent) error {
	query := `
		INSERT INTO events_processed (
			id, tenant_id, external_customer_id, customer_id, event_name, source, 
			timestamp, ingested_at, properties, processed_at, environment_id,
			subscription_id, price_id, meter_id, aggregation_field, 
			aggregation_field_value, quantity, cost, currency
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
		event.ProcessedAt,
		event.EnvironmentID,
		event.SubscriptionID,
		event.PriceID,
		event.MeterID,
		event.AggregationField,
		event.AggregationFieldValue,
		event.Quantity,
		event.Cost,
		event.Currency,
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
func (r *ProcessedEventRepository) BulkInsertProcessedEvents(ctx context.Context, events []*events.ProcessedEvent) error {
	if len(events) == 0 {
		return nil
	}

	// Split events in batches of 100
	eventsBatches := lo.Chunk(events, 100)

	for _, eventsBatch := range eventsBatches {
		// Prepare batch statement
		batch, err := r.store.GetConn().PrepareBatch(ctx, `
			INSERT INTO events_processed (
				id, tenant_id, external_customer_id, customer_id, event_name, source, 
				timestamp, ingested_at, properties, processed_at, environment_id,
				subscription_id, price_id, meter_id, aggregation_field, 
				aggregation_field_value, quantity, cost, currency
			)
		`)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to prepare batch for processed events").
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
				event.ProcessedAt,
				event.EnvironmentID,
				event.SubscriptionID,
				event.PriceID,
				event.MeterID,
				event.AggregationField,
				event.AggregationFieldValue,
				event.Quantity,
				event.Cost,
				event.Currency,
			)

			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to append processed event to batch").
					WithReportableDetails(map[string]interface{}{
						"event_id": event.ID,
					}).
					Mark(ierr.ErrDatabase)
			}
		}

		// Send batch
		if err := batch.Send(); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to execute batch insert for processed events").
				WithReportableDetails(map[string]interface{}{
					"event_count": len(events),
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	return nil
}

// GetProcessedEvents retrieves processed events based on the provided parameters
func (r *ProcessedEventRepository) GetProcessedEvents(ctx context.Context, params *events.GetProcessedEventsParams) ([]*events.ProcessedEvent, uint64, error) {
	query := `
		SELECT 
			id, tenant_id, external_customer_id, customer_id, event_name, source, 
			timestamp, ingested_at, properties, processed_at, environment_id,
			subscription_id, price_id, meter_id, aggregation_field, 
			aggregation_field_value, quantity, cost, currency
		FROM events_processed
		WHERE tenant_id = ?
		AND timestamp >= ?
		AND timestamp <= ?
	`

	countQuery := `
		SELECT COUNT(*)
		FROM events_processed
		WHERE tenant_id = ?
		AND timestamp >= ?
		AND timestamp <= ?
	`

	args := []interface{}{types.GetTenantID(ctx), params.StartTime, params.EndTime}

	// Add filters
	if params.CustomerID != "" {
		query += " AND customer_id = ?"
		countQuery += " AND customer_id = ?"
		args = append(args, params.CustomerID)
	}

	if params.SubscriptionID != "" {
		query += " AND subscription_id = ?"
		countQuery += " AND subscription_id = ?"
		args = append(args, params.SubscriptionID)
	}

	if params.MeterID != "" {
		query += " AND meter_id = ?"
		countQuery += " AND meter_id = ?"
		args = append(args, params.MeterID)
	}

	if params.PriceID != "" {
		query += " AND price_id = ?"
		countQuery += " AND price_id = ?"
		args = append(args, params.PriceID)
	}

	if params.OnlyProcessed {
		query += " AND processed_at IS NOT NULL"
		countQuery += " AND processed_at IS NOT NULL"
	}

	if params.OnlyUnprocessed {
		query += " AND processed_at IS NULL"
		countQuery += " AND processed_at IS NULL"
	}

	// Add FINAL modifier to handle ReplacingMergeTree
	query += " FINAL"
	countQuery += " FINAL"

	// Add pagination
	if params.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, params.Limit)

		if params.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, params.Offset)
		}
	}

	// Execute query
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, 0, ierr.WithError(err).
			WithHint("Failed to query processed events").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var processedEvents []*events.ProcessedEvent

	for rows.Next() {
		var event events.ProcessedEvent
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
			&event.PriceID,
			&event.MeterID,
			&event.AggregationField,
			&event.AggregationFieldValue,
			&event.Quantity,
			&event.Cost,
			&event.Currency,
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
					WithHint("Failed to unmarshal event properties").
					Mark(ierr.ErrValidation)
			}
		} else {
			event.Properties = make(map[string]interface{})
		}

		processedEvents = append(processedEvents, &event)
	}

	// Get total count if requested
	var total uint64
	if params.CountTotal {
		if err := r.store.GetConn().QueryRow(ctx, countQuery, args[:len(args)-2]...).Scan(&total); err != nil {
			return nil, 0, ierr.WithError(err).
				WithHint("Failed to count processed events").
				Mark(ierr.ErrDatabase)
		}
	}

	return processedEvents, total, nil
}

// GetUsageSummary calculates usage summaries based on pre-computed costs
func (r *ProcessedEventRepository) GetUsageSummary(ctx context.Context, params *events.UsageSummaryParams) (decimal.Decimal, error) {
	query := `
		SELECT SUM(cost) as total_cost
		FROM events_processed
		WHERE tenant_id = ?
		AND timestamp >= ?
		AND timestamp <= ?
		AND processed_at IS NOT NULL
	`
	args := []interface{}{types.GetTenantID(ctx), params.StartTime, params.EndTime}

	// Add filters
	if params.CustomerID != "" {
		query += " AND customer_id = ?"
		args = append(args, params.CustomerID)
	}

	if params.SubscriptionID != "" {
		query += " AND subscription_id = ?"
		args = append(args, params.SubscriptionID)
	}

	if params.PriceID != "" {
		query += " AND price_id = ?"
		args = append(args, params.PriceID)
	}

	if params.MeterID != "" {
		query += " AND meter_id = ?"
		args = append(args, params.MeterID)
	}

	// Handle duplicates by using ReplacingMergeTree version field
	// The FINAL modifier ensures that for each set of rows with the same primary key,
	// only the one with the largest version number is returned
	query += " FINAL"

	var totalCost decimal.Decimal
	err := r.store.GetConn().QueryRow(ctx, query, args...).Scan(&totalCost)
	if err != nil {
		return decimal.Zero, ierr.WithError(err).
			WithHint("Failed to calculate usage summary").
			Mark(ierr.ErrDatabase)
	}

	return totalCost, nil
}

// FindUnprocessedEvents finds events that need to be processed
func (r *ProcessedEventRepository) FindUnprocessedEvents(ctx context.Context, customerID, subscriptionID string) ([]*events.ProcessedEvent, error) {
	if customerID == "" && subscriptionID == "" {
		return nil, ierr.NewError("at least one filter is required").
			WithHint("Either customer ID or subscription ID is required").
			Mark(ierr.ErrValidation)
	}

	query := `
		SELECT 
			id, tenant_id, external_customer_id, customer_id, 
			event_name, source, timestamp, ingested_at, properties
		FROM events_processed
		WHERE tenant_id = ?
		AND processed_at IS NULL
	`
	args := []interface{}{types.GetTenantID(ctx)}

	// Add filters
	if customerID != "" {
		query += " AND customer_id = ?"
		args = append(args, customerID)
	}

	if subscriptionID != "" {
		query += " AND subscription_id = ?"
		args = append(args, subscriptionID)
	}

	// Add FINAL modifier for ReplacingMergeTree
	query += " FINAL"

	// Execute query
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to query unprocessed events").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var eventsList []*events.ProcessedEvent

	for rows.Next() {
		var event events.ProcessedEvent
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
		)

		if err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to scan event").
				Mark(ierr.ErrDatabase)
		}

		// Parse properties
		if propertiesJSON != "" {
			if err := json.Unmarshal([]byte(propertiesJSON), &event.Properties); err != nil {
				return nil, ierr.WithError(err).
					WithHint("Failed to unmarshal properties").
					Mark(ierr.ErrValidation)
			}
		} else {
			event.Properties = make(map[string]interface{})
		}

		eventsList = append(eventsList, &event)
	}

	return eventsList, nil
}
