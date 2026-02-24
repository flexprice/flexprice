package clickhouse

import (
	"context"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type RawEventRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewRawEventRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) events.RawEventRepository {
	return &RawEventRepository{store: store, logger: logger}
}

// FindRawEvents finds raw events with filtering and keyset pagination
// Query is optimized for the table structure:
// - PRIMARY KEY: (tenant_id, environment_id, external_customer_id, timestamp)
// - ORDER BY: (tenant_id, environment_id, external_customer_id, timestamp, event_name, id)
// - PARTITION BY: toYYYYMMDD(timestamp)
// - ENGINE: ReplacingMergeTree(version)
func (r *RawEventRepository) FindRawEvents(ctx context.Context, params *events.FindRawEventsParams) ([]*events.RawEvent, error) {
	span := StartRepositorySpan(ctx, "raw_event", "find_raw_events", map[string]interface{}{
		"batch_size":           params.BatchSize,
		"external_customer_id": params.ExternalCustomerID,
	})
	defer FinishSpan(span)

	// Get tenant and environment ID from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Build query with filters following primary key order for optimal index usage
	query := `
		SELECT 
			id, tenant_id, environment_id, external_customer_id, event_name, 
			source, payload, field1, field2, field3, field4, field5, 
			field6, field7, field8, field9, field10, timestamp, ingested_at, 
			version, sign
		FROM raw_events
		WHERE tenant_id = ?
		AND environment_id = ?
	`

	args := []interface{}{tenantID, environmentID}

	// Add filters if provided - order matters for index usage
	// Follow the primary key order: tenant_id, environment_id, external_customer_id, timestamp
	if params.ExternalCustomerID != "" {
		query += " AND external_customer_id = ?"
		args = append(args, params.ExternalCustomerID)
	}

	if len(params.ExternalCustomerIDs) > 0 {
		query += " AND external_customer_id IN ?"
		args = append(args, params.ExternalCustomerIDs)
	}

	if !params.StartTime.IsZero() {
		query += " AND timestamp >= ?"
		args = append(args, params.StartTime)
	}

	if !params.EndTime.IsZero() {
		query += " AND timestamp <= ?"
		args = append(args, params.EndTime)
	}

	if params.EventName != "" {
		query += " AND event_name = ?"
		args = append(args, params.EventName)
	}

	if len(params.EventIDs) > 0 {
		query += " AND id IN ?"
		args = append(args, params.EventIDs)
	}

	// Add sorting for consistent ordering
	query += " ORDER BY timestamp DESC, event_name DESC, id DESC"

	// Add OFFSET and LIMIT for simple pagination
	if params.Offset > 0 {
		query += " LIMIT ? OFFSET ?"
		if params.BatchSize > 0 {
			args = append(args, params.BatchSize, params.Offset)
		} else {
			args = append(args, 1000, params.Offset)
		}
	} else {
		// No offset, just limit
		if params.BatchSize > 0 {
			query += " LIMIT ?"
			args = append(args, params.BatchSize)
		} else {
			query += " LIMIT 1000"
		}
	}

	r.logger.Infow("executing find raw events query",
		"query", query,
		"args", args,
		"external_customer_id", params.ExternalCustomerID,
		"event_name", params.EventName,
		"batch_size", params.BatchSize,
		"offset", params.Offset,
	)

	// Execute the query
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to query raw events").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var eventsList []*events.RawEvent
	for rows.Next() {
		var event events.RawEvent

		err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.EnvironmentID,
			&event.ExternalCustomerID,
			&event.EventName,
			&event.Source,
			&event.Payload,
			&event.Field1,
			&event.Field2,
			&event.Field3,
			&event.Field4,
			&event.Field5,
			&event.Field6,
			&event.Field7,
			&event.Field8,
			&event.Field9,
			&event.Field10,
			&event.Timestamp,
			&event.IngestedAt,
			&event.Version,
			&event.Sign,
		)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan raw event").
				Mark(ierr.ErrDatabase)
		}

		eventsList = append(eventsList, &event)
	}

	// Check for errors that occurred during iteration
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Error occurred during row iteration").
			Mark(ierr.ErrDatabase)
	}

	r.logger.Infow("fetched raw events from clickhouse",
		"count", len(eventsList),
		"expected_batch_size", params.BatchSize,
		"offset", params.Offset,
	)

	SetSpanSuccess(span)
	return eventsList, nil
}

// FindUnprocessedRawEvents finds raw events that haven't been processed yet
// Uses ANTI JOIN with feature_usage table to exclude already processed events
// This is useful for catching up on missed events without creating duplicates
func (r *RawEventRepository) FindUnprocessedRawEvents(ctx context.Context, params *events.FindRawEventsParams) ([]*events.RawEvent, error) {
	span := StartRepositorySpan(ctx, "raw_event", "find_unprocessed_raw_events", map[string]interface{}{
		"batch_size":           params.BatchSize,
		"external_customer_id": params.ExternalCustomerID,
	})
	defer FinishSpan(span)

	// Get tenant and environment ID from context
	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Use ANTI JOIN with feature_usage table for better performance with ClickHouse
	// This finds raw events that don't have a corresponding entry in the feature_usage table
	query := `
		SELECT 
			r.id, r.tenant_id, r.environment_id, r.external_customer_id, r.event_name, 
			r.source, r.payload, r.field1, r.field2, r.field3, r.field4, r.field5, 
			r.field6, r.field7, r.field8, r.field9, r.field10, r.timestamp, r.ingested_at, 
			r.version, r.sign
		FROM raw_events r
		ANTI JOIN (
			SELECT id, tenant_id, environment_id
			FROM feature_usage
			WHERE tenant_id = ?
			AND environment_id = ?
		) AS e
		ON r.id = e.id AND r.tenant_id = e.tenant_id AND r.environment_id = e.environment_id
		WHERE r.tenant_id = ?
		AND r.environment_id = ?
	`

	args := []interface{}{tenantID, environmentID, tenantID, environmentID}

	// Add filters if provided - order matters for index usage
	if params.ExternalCustomerID != "" {
		query += " AND r.external_customer_id = ?"
		args = append(args, params.ExternalCustomerID)
	}

	if len(params.ExternalCustomerIDs) > 0 {
		query += " AND r.external_customer_id IN ?"
		args = append(args, params.ExternalCustomerIDs)
	}

	if !params.StartTime.IsZero() {
		query += " AND r.timestamp >= ?"
		args = append(args, params.StartTime)
	}

	if !params.EndTime.IsZero() {
		query += " AND r.timestamp <= ?"
		args = append(args, params.EndTime)
	}

	if params.EventName != "" {
		query += " AND r.event_name = ?"
		args = append(args, params.EventName)
	}

	if len(params.EventIDs) > 0 {
		query += " AND r.id IN ?"
		args = append(args, params.EventIDs)
	}

	// Add sorting for consistent ordering
	query += " ORDER BY r.timestamp DESC, r.event_name DESC, r.id DESC"

	// Add OFFSET and LIMIT for simple pagination
	if params.Offset > 0 {
		query += " LIMIT ? OFFSET ?"
		if params.BatchSize > 0 {
			args = append(args, params.BatchSize, params.Offset)
		} else {
			args = append(args, 1000, params.Offset)
		}
	} else {
		// No offset, just limit
		if params.BatchSize > 0 {
			query += " LIMIT ?"
			args = append(args, params.BatchSize)
		} else {
			query += " LIMIT 1000"
		}
	}

	r.logger.Debugw("executing find unprocessed raw events query",
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
			WithHint("Failed to query unprocessed raw events").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var eventsList []*events.RawEvent
	for rows.Next() {
		var event events.RawEvent

		err := rows.Scan(
			&event.ID,
			&event.TenantID,
			&event.EnvironmentID,
			&event.ExternalCustomerID,
			&event.EventName,
			&event.Source,
			&event.Payload,
			&event.Field1,
			&event.Field2,
			&event.Field3,
			&event.Field4,
			&event.Field5,
			&event.Field6,
			&event.Field7,
			&event.Field8,
			&event.Field9,
			&event.Field10,
			&event.Timestamp,
			&event.IngestedAt,
			&event.Version,
			&event.Sign,
		)
		if err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to scan unprocessed raw event").
				Mark(ierr.ErrDatabase)
		}

		eventsList = append(eventsList, &event)
	}

	r.logger.Infow("found unprocessed raw events",
		"count", len(eventsList),
		"external_customer_id", params.ExternalCustomerID,
		"event_name", params.EventName,
	)

	SetSpanSuccess(span)
	return eventsList, nil
}
