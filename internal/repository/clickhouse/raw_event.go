package clickhouse

import (
	"context"
	"time"

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
		"batch_size":            params.BatchSize,
		"external_customer_ids": params.ExternalCustomerIDs,
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

	if len(params.EventNames) > 0 {
		query += " AND event_name IN ?"
		args = append(args, params.EventNames)
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
		"external_customer_ids", params.ExternalCustomerIDs,
		"event_names", params.EventNames,
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

// FindUnprocessedRawEvents finds raw events that haven't been processed yet.
//
// Strategy (2-step batch, keyset pagination):
//
//	Step A — Narrow scan: LEFT ANTI JOIN to find only unprocessed IDs.
//	         Reads 3 columns (timestamp, id, event_name) — cheap columnar I/O.
//	         Uses keyset pagination (timestamp, id) < (last_ts, last_id) instead
//	         of OFFSET, so every page costs the same regardless of depth.
//
//	Step B — Wide fetch: fetch all 20 columns only for the batch of IDs found
//	         in Step A. Bounded to BatchSize rows, never scans the full range.
//
// Caller drives pagination by passing params.KeysetCursor (nil = first batch).
// When the returned cursor is nil, there are no more batches.
func (r *RawEventRepository) FindUnprocessedRawEvents(
	ctx context.Context,
	params *events.FindRawEventsParams,
) ([]*events.RawEvent, *events.KeysetCursor, error) {
	span := StartRepositorySpan(ctx, "raw_event", "find_unprocessed_raw_events", map[string]interface{}{
		"batch_size":            params.BatchSize,
		"external_customer_ids": params.ExternalCustomerIDs,
		"has_cursor":            params.KeysetCursor != nil,
	})
	defer FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	batchSize := params.BatchSize
	if batchSize <= 0 {
		batchSize = 1000
	}

	// ── Step A: find unprocessed IDs (narrow scan) ───────────────────────────
	//
	// The events subquery filters by timestamp so ClickHouse only loads the
	// relevant time-window rows into the ANTI JOIN hash table, not all events
	// for the tenant. On a tenant with 100M lifetime events but a 2-week query
	// window this alone can reduce hash table size by 50x+.
	//
	// No DISTINCT — if events.id is unique per (tenant, environment) the extra
	// dedup pass just wastes CPU.

	stepAQuery := `
		SELECT r.timestamp, r.id, r.event_name
		FROM raw_events r
		LEFT ANTI JOIN (
			SELECT id
			FROM events
			WHERE tenant_id      = ?
			  AND environment_id = ?
			  AND timestamp >= ?
			  AND timestamp <  ?
		) e ON r.id = e.id
		WHERE r.tenant_id      = ?
		  AND r.environment_id = ?
		  AND r.timestamp >= ?
		  AND r.timestamp <  ?
	`

	// Argument order: subquery first, then outer WHERE — mirrors ? placeholders above.
	stepAArgs := []interface{}{
		// subquery (events side) — scopes the hash table to the time window
		tenantID, environmentID, params.StartTime, params.EndTime,
		// outer WHERE (raw_events side)
		tenantID, environmentID, params.StartTime, params.EndTime,
	}

	// Optional filters — order matches ClickHouse primary key for index usage.
	// Add customer filter before event_name so partition pruning kicks in first.
	if len(params.ExternalCustomerIDs) > 0 {
		stepAQuery += " AND r.external_customer_id IN ?"
		stepAArgs = append(stepAArgs, params.ExternalCustomerIDs)
	}

	if len(params.EventNames) > 0 {
		stepAQuery += " AND r.event_name IN ?"
		stepAArgs = append(stepAArgs, params.EventNames)
	}

	if len(params.EventIDs) > 0 {
		stepAQuery += " AND r.id IN ?"
		stepAArgs = append(stepAArgs, params.EventIDs)
	}

	// Keyset pagination: tuple comparison on (timestamp, id) lets ClickHouse
	// use the sort key directly. This replaces OFFSET which at page N would
	// scan and discard N*batchSize rows — catastrophic at billions of events.
	if params.KeysetCursor != nil {
		stepAQuery += " AND (r.timestamp, r.id) < (?, ?)"
		stepAArgs = append(stepAArgs, params.KeysetCursor.LastTimestamp, params.KeysetCursor.LastID)
	}

	stepAQuery += " ORDER BY r.timestamp DESC, r.id DESC LIMIT ?"
	stepAArgs = append(stepAArgs, batchSize)

	r.logger.Debugw("executing step A: find unprocessed IDs",
		"query", stepAQuery,
		"tenant_id", tenantID,
		"external_customer_ids", params.ExternalCustomerIDs,
		"event_names", params.EventNames,
		"batch_size", batchSize,
		"has_cursor", params.KeysetCursor != nil,
	)

	rowsA, err := r.store.GetConn().Query(ctx, stepAQuery, stepAArgs...)
	if err != nil {
		SetSpanError(span, err)
		return nil, nil, ierr.WithError(err).
			WithHint("Failed to query unprocessed event IDs (step A)").
			Mark(ierr.ErrDatabase)
	}
	defer rowsA.Close()

	var (
		ids        []string
		nextCursor *events.KeysetCursor // will be set to last row if batch is full
	)

	for rowsA.Next() {
		var (
			ts        time.Time
			id        string
			eventName string
		)
		if err := rowsA.Scan(&ts, &id, &eventName); err != nil {
			SetSpanError(span, err)
			return nil, nil, ierr.WithError(err).
				WithHint("Failed to scan unprocessed event ID (step A)").
				Mark(ierr.ErrDatabase)
		}
		ids = append(ids, id)
		// Keep updating — after the loop this holds the last (oldest) row,
		// which becomes the cursor for the next batch.
		nextCursor = &events.KeysetCursor{
			LastTimestamp: ts,
			LastID:        id,
		}
	}

	r.logger.Debugw("step A complete",
		"unprocessed_id_count", len(ids),
		"has_next_cursor", nextCursor != nil,
	)

	// No unprocessed events found — return early, no cursor.
	if len(ids) == 0 {
		SetSpanSuccess(span)
		return nil, nil, nil
	}

	// If we got fewer rows than batchSize, we've exhausted the dataset.
	// Signal that to the caller by returning a nil cursor.
	if len(ids) < batchSize {
		nextCursor = nil
	}

	// ── Step B: fetch full rows for the IDs found above ──────────────────────
	//
	// Bounded to len(ids) rows (≤ batchSize). The expensive 20-column wide
	// read only touches exactly the rows we know need processing, not a range.
	// tenant_id + environment_id help ClickHouse prune partitions even with
	// a point lookup on id.

	stepBQuery := `
		SELECT
			id, tenant_id, environment_id, external_customer_id, event_name,
			source, payload,
			field1, field2, field3, field4, field5,
			field6, field7, field8, field9, field10,
			timestamp, ingested_at, version, sign
		FROM raw_events
		WHERE tenant_id      = ?
		  AND environment_id = ?
		  AND id IN ?
	`
	stepBArgs := []interface{}{tenantID, environmentID, ids}

	r.logger.Debugw("executing step B: fetch full event rows",
		"id_count", len(ids),
	)

	rowsB, err := r.store.GetConn().Query(ctx, stepBQuery, stepBArgs...)
	if err != nil {
		SetSpanError(span, err)
		return nil, nil, ierr.WithError(err).
			WithHint("Failed to fetch full raw event rows (step B)").
			Mark(ierr.ErrDatabase)
	}
	defer rowsB.Close()

	eventsList := make([]*events.RawEvent, 0, len(ids))

	for rowsB.Next() {
		var event events.RawEvent
		if err := rowsB.Scan(
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
		); err != nil {
			SetSpanError(span, err)
			return nil, nil, ierr.WithError(err).
				WithHint("Failed to scan raw event row (step B)").
				Mark(ierr.ErrDatabase)
		}
		eventsList = append(eventsList, &event)
	}

	r.logger.Infow("found unprocessed raw events",
		"count", len(eventsList),
		"external_customer_ids", params.ExternalCustomerIDs,
		"event_names", params.EventNames,
		"has_next_cursor", nextCursor != nil,
	)

	SetSpanSuccess(span)
	return eventsList, nextCursor, nil
}
