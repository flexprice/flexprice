package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"time"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	// maxMutationQuerySize fits the inline id list a feature-narrowed events
	// delete carries. Set on the connection, not in SETTINGS: the parser reads
	// the limit before it reaches a SETTINGS clause.
	maxMutationQuerySize = 1 << 30

	// mutationDeadline bounds the wait; without it a stalled mutation holds the
	// state row DELETING and blocks that customer's invoicing forever.
	mutationDeadline     = 10 * time.Minute
	mutationPollInterval = 2 * time.Second

	// recordBatchSize is how many ids one audit INSERT ... SELECT carries.
	recordBatchSize = 1000

	// mutationKillTimeout bounds the KILL issued for a mutation we stopped waiting
	// for. Detached from the caller, which has usually gone by then.
	mutationKillTimeout = 30 * time.Second
)

// EventDeletionLogRepository hard-deletes events and their meter usage, recording
// every deleted event first.
type EventDeletionLogRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
	qb     *MeterUsageQueryBuilder
}

func NewEventDeletionLogRepository(store *clickhouse.ClickHouseStore, log *logger.Logger) events.DeletedEventsLogRepository {
	return &EventDeletionLogRepository{
		store:  store,
		logger: log,
		qb:     NewMeterUsageQueryBuilder(),
	}
}

// meterUsageParams is the meter_usage clause. ids replace the scope's own event
// id filter: every step after the resolve keys on the resolved set.
func meterUsageParams(scope *events.MeterUsageQueryParams, ids []string) *events.MeterUsageQueryParams {
	narrowed := *scope
	if len(ids) > 0 {
		narrowed.IDs = ids
	}
	return &narrowed
}

// baseParams drops the meter filter. It is the only form the events table can be
// queried with, since events has no meter column.
func baseParams(scope *events.MeterUsageQueryParams, ids []string) *events.MeterUsageQueryParams {
	params := meterUsageParams(scope, ids)
	params.MeterIDs = nil
	params.MeterID = ""
	return params
}

// isMeterNarrowed reports whether the request named features.
func isMeterNarrowed(scope *events.MeterUsageQueryParams) bool {
	return len(scope.MeterIDs) > 0 || scope.MeterID != ""
}

func (r *EventDeletionLogRepository) ResolveEventIDs(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
) (*events.DeletionTargets, error) {
	span := StartRepositorySpan(ctx, "event_deletion", "resolve_event_ids", map[string]interface{}{
		"customer_count": len(scope.ExternalCustomerIDs),
		"meter_count":    len(scope.MeterIDs),
	})
	defer FinishSpan(span)

	// FINAL and count() per id, so the row count matches what the audit insert
	// writes and the mutation removes, without reading the audit table back.
	where, args := r.qb.BuildWhereClause(meterUsageParams(scope, nil))
	rows, err := r.store.GetConn().Query(ctx, fmt.Sprintf(`
		SELECT id, count() AS meter_rows
		FROM meter_usage FINAL
		WHERE %s
		GROUP BY id
		LIMIT %d
		SETTINGS do_not_merge_across_partitions_select_final = 1, %s
	`, where, events.MaxEventsPerRequest+1, maxMemoryUsageSetting), args...)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to resolve the meter usage in scope for deletion").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	targets := &events.DeletionTargets{Metered: make([]string, 0), Unmetered: make([]string, 0)}
	for rows.Next() {
		var id string
		var meterRows uint64
		if err := rows.Scan(&id, &meterRows); err != nil {
			SetSpanError(span, err)
			return nil, ierr.WithError(err).
				WithHint("Failed to read the meter usage in scope for deletion").
				Mark(ierr.ErrDatabase)
		}
		targets.Metered = append(targets.Metered, id)
		targets.MeterUsageRows += meterRows
	}
	if err := rows.Err(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to read the meter usage in scope for deletion").
			Mark(ierr.ErrDatabase)
	}

	// A meter-narrowed request can never match an event that produced no meter
	// usage, so there is nothing to look for in events.
	if isMeterNarrowed(scope) {
		SetSpanSuccess(span)
		return targets, nil
	}

	baseWhere, baseArgs := r.qb.BuildWhereClause(baseParams(scope, nil))
	all, err := r.selectIDs(ctx, fmt.Sprintf(`
		SELECT DISTINCT id
		FROM events
		WHERE %s
		LIMIT %d
		SETTINGS %s
	`, baseWhere, events.MaxEventsPerRequest+1, maxMemoryUsageSetting), baseArgs)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to resolve the events in scope for deletion").
			Mark(ierr.ErrDatabase)
	}

	meteredSet := lo.SliceToMap(targets.Metered, func(id string) (string, struct{}) {
		return id, struct{}{}
	})
	for _, id := range all {
		if _, ok := meteredSet[id]; !ok {
			targets.Unmetered = append(targets.Unmetered, id)
		}
	}

	SetSpanSuccess(span)
	return targets, nil
}

func (r *EventDeletionLogRepository) FindStrandedMeters(ctx context.Context, scope *events.MeterUsageQueryParams, eventIDs []string) (*events.StrandedMeters, error) {
	if !isMeterNarrowed(scope) || len(eventIDs) == 0 {
		return &events.StrandedMeters{}, nil
	}

	span := StartRepositorySpan(ctx, "event_deletion", "find_stranded_meters", map[string]interface{}{
		"event_count": len(eventIDs),
	})
	defer FinishSpan(span)

	// Narrow form: an event whose every meter was named passes.
	// No FINAL: count(DISTINCT id) and groupUniqArray collapse versions already.
	where, args := r.qb.BuildWhereClause(baseParams(scope, eventIDs))
	args = append(args, scope.MeterIDs)

	query := fmt.Sprintf(`
		SELECT count(DISTINCT id) AS event_count, groupUniqArray(meter_id) AS meters_involved
		FROM meter_usage
		WHERE %s AND meter_id NOT IN (?)
		SETTINGS %s
	`, where, maxMemoryUsageSetting)

	var result events.StrandedMeters
	if err := r.store.GetConn().QueryRow(ctx, query, args...).Scan(&result.EventCount, &result.MetersInvolved); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to check whether the deletion would strand usage on another meter").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return &result, nil
}

// RecordDeletedEvents writes the audit rows for both id sets, batched. An insert
// appends a part and is cheap, unlike the mutations, which is why these batch and
// the deletes do not.
func (r *EventDeletionLogRepository) RecordDeletedEvents(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
	metered, unmetered []string,
	stamp events.DeletionStamp,
) error {
	for _, batch := range lo.Chunk(metered, recordBatchSize) {
		if err := r.recordMetered(ctx, scope, batch, stamp); err != nil {
			return err
		}
	}
	for _, batch := range lo.Chunk(unmetered, recordBatchSize) {
		if err := r.recordUnmetered(ctx, scope, batch, stamp); err != nil {
			return err
		}
	}

	r.logger.Info(ctx, "recorded the events about to be deleted",
		"request_id", stamp.RequestID,
		"metered_count", len(metered),
		"unmetered_count", len(unmetered),
	)
	return nil
}

func (r *EventDeletionLogRepository) recordMetered(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
	eventIDs []string,
	stamp events.DeletionStamp,
) error {
	if len(eventIDs) == 0 {
		return nil
	}

	span := StartRepositorySpan(ctx, "event_deletion", "record_metered_events", map[string]interface{}{
		"request_id":  stamp.RequestID,
		"event_count": len(eventIDs),
	})
	defer FinishSpan(span)

	where, whereArgs := r.qb.BuildWhereClause(meterUsageParams(scope, eventIDs))

	// FINAL on meter_usage: an uncollapsed duplicate would be a duplicate audit row.
	// LIMIT 1 BY on the events side: timestamp is in its sort key, so FINAL alone
	// leaves the same id re-ingested at a corrected timestamp as two rows.
	// The subquery repeats the predicate by hand because predicate propagation
	// across a join is unavailable, and without it the whole table is scanned.
	query := fmt.Sprintf(`
		INSERT INTO deleted_events_logs (
			event_id, meter_id, tenant_id, environment_id, external_customer_id,
			event_name, source, event_timestamp, event_ingested_at,
			qty_total, unique_hash, properties, deleted_at, deleted_by, request_id
		)
		SELECT m.id, m.meter_id, m.tenant_id, m.environment_id, m.external_customer_id,
			m.event_name, m.source, e.ev_timestamp, e.ev_ingested_at,
			m.qty_total, m.unique_hash, %s,
			?, ?, ?
		FROM meter_usage AS m FINAL
		LEFT JOIN (
			SELECT id AS ev_id, timestamp AS ev_timestamp, ingested_at AS ev_ingested_at
			FROM events FINAL
			WHERE tenant_id = ? AND environment_id = ?
				AND timestamp >= ? AND timestamp < ?
				AND id IN (?)
			ORDER BY ingested_at DESC
			LIMIT 1 BY ev_id
		) AS e ON m.id = e.ev_id
		WHERE %s
		SETTINGS %s
	`, propertiesExpr(stamp.SkipPayload, "m.properties"), where, maxMemoryUsageSetting)

	args := []interface{}{stamp.DeletedAt.UTC(), stamp.DeletedBy, stamp.RequestID,
		scope.TenantID, scope.EnvironmentID, scope.StartTime.UTC(), scope.EndTime.UTC(), eventIDs}
	args = append(args, whereArgs...)

	if err := r.store.GetConn().Exec(ctx, query, args...); err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to record the meter usage about to be deleted").
			WithReportableDetails(map[string]interface{}{"request_id": stamp.RequestID}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

func (r *EventDeletionLogRepository) recordUnmetered(
	ctx context.Context,
	scope *events.MeterUsageQueryParams,
	eventIDs []string,
	stamp events.DeletionStamp,
) error {
	if len(eventIDs) == 0 {
		return nil
	}

	span := StartRepositorySpan(ctx, "event_deletion", "record_unmetered_events", map[string]interface{}{
		"request_id":  stamp.RequestID,
		"event_count": len(eventIDs),
	})
	defer FinishSpan(span)

	where, whereArgs := r.qb.BuildWhereClause(baseParams(scope, eventIDs))

	// LIMIT 1 BY id for the same reason as above: one audit row per id.
	query := fmt.Sprintf(`
		INSERT INTO deleted_events_logs (
			event_id, meter_id, tenant_id, environment_id, external_customer_id,
			event_name, source, event_timestamp, event_ingested_at,
			qty_total, unique_hash, properties, deleted_at, deleted_by, request_id
		)
		SELECT id, '', tenant_id, environment_id, external_customer_id,
			event_name, ifNull(source, ''), timestamp, ingested_at,
			CAST(0 AS Decimal(25, 15)), '', %s,
			?, ?, ?
		FROM events FINAL
		WHERE %s
		ORDER BY ingested_at DESC
		LIMIT 1 BY id
		SETTINGS %s
	`, propertiesExpr(stamp.SkipPayload, "ifNull(properties, '')"), where, maxMemoryUsageSetting)

	args := []interface{}{stamp.DeletedAt.UTC(), stamp.DeletedBy, stamp.RequestID}
	args = append(args, whereArgs...)

	if err := r.store.GetConn().Exec(ctx, query, args...); err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to record the events about to be deleted").
			WithReportableDetails(map[string]interface{}{"request_id": stamp.RequestID}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return nil
}

// propertiesExpr writes ” when the request set skip_payload.
func propertiesExpr(skipPayload bool, column string) string {
	if skipPayload {
		return "''"
	}
	return column
}

func (r *EventDeletionLogRepository) DeleteMeterUsage(ctx context.Context, scope *events.MeterUsageQueryParams, eventIDs []string) error {
	where, args := r.qb.BuildWhereClause(meterUsageParams(scope, eventIDs))
	return r.mutate(ctx, "meter_usage", where, args)
}

func (r *EventDeletionLogRepository) DeleteEvents(ctx context.Context, scope *events.MeterUsageQueryParams, eventIDs []string) error {
	where, args := r.qb.BuildWhereClause(baseParams(scope, eventIDs))
	return r.mutate(ctx, "events", where, args)
}

// mutate submits one heavyweight delete and waits for it. ALTER TABLE ... DELETE,
// not DELETE FROM: a lightweight delete fails on a table carrying a projection,
// and production events carries one.
func (r *EventDeletionLogRepository) mutate(ctx context.Context, table, where string, args []interface{}) error {
	span := StartRepositorySpan(ctx, "event_deletion", "mutate", map[string]interface{}{"table": table})
	defer FinishSpan(span)

	// A no-op predicate carrying a unique marker. clickhouse-go binds client
	// side, so the marker lands in system.mutations.command and the wait below
	// can tell this mutation apart from a concurrent one on the same table.
	marker := types.GenerateUUID()
	query := fmt.Sprintf("ALTER TABLE %s DELETE WHERE %s AND ? != ''", table, where)
	args = append(args, marker)

	// mutations_sync = 0 submits and returns: a blocking submit would hit the
	// client read deadline while still succeeding on the server.
	execCtx := clickhouse_go.Context(ctx, clickhouse_go.WithSettings(clickhouse_go.Settings{
		"mutations_sync": 0,
		"max_query_size": maxMutationQuerySize,
	}))

	if err := r.store.GetConn().Exec(execCtx, query, args...); err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to submit the delete mutation").
			WithReportableDetails(map[string]interface{}{
				"reason":     types.EventDeletionReasonMutationFailed,
				"request_id": types.GetRequestID(ctx),
			}).
			Mark(ierr.ErrDatabase)
	}

	r.logger.Info(ctx, "submitted delete mutation", "table", table)

	if err := r.waitForMutation(ctx, table, marker); err != nil {
		SetSpanError(span, err)
		// A mutation that failed server side has already stopped. One we gave up
		// waiting for has not, and would keep rewriting parts unattended.
		if errors.Is(err, types.ErrMutationTimeout) || ctx.Err() != nil {
			r.kill(ctx, table, marker)
		}
		return err
	}

	SetSpanSuccess(span)
	return nil
}

// waitForMutation polls until this request's own mutation is done. Scoped by the
// marker: matching on time alone would pick up a concurrent deletion's mutation
// and report its failure, or its runtime, as ours.
func (r *EventDeletionLogRepository) waitForMutation(ctx context.Context, table, marker string) error {
	query := `
		SELECT count() AS total, countIf(is_done) AS done, max(latest_fail_reason) AS fail_reason
		FROM system.mutations
		WHERE database = currentDatabase() AND table = ? AND position(command, ?) > 0
	`

	deadline := types.Now().Add(mutationDeadline)
	for {
		var total, done uint64
		var failReason string
		if err := r.store.GetConn().QueryRow(ctx, query, table, marker).Scan(&total, &done, &failReason); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to read the delete mutation status").
				WithReportableDetails(map[string]interface{}{"table": table}).
				Mark(ierr.ErrDatabase)
		}

		if failReason != "" {
			// The engine's own message stays server side: it names internal tables
			// and part paths the caller can neither read nor act on.
			r.logger.Error(ctx, "delete mutation failed",
				"error", failReason,
				"table", table,
			)
			return ierr.NewError("delete mutation failed").
				WithHint("The delete did not complete. Everything recorded for this request_id is still present").
				WithReportableDetails(map[string]interface{}{
					"reason":     types.EventDeletionReasonMutationFailed,
					"request_id": types.GetRequestID(ctx),
				}).
				Mark(ierr.ErrDatabase)
		}

		if total > 0 && done == total {
			return nil
		}

		if types.Now().After(deadline) {
			return ierr.WithError(types.ErrMutationTimeout).
				WithHint("The delete took too long and was cancelled. Retry it for the same period").
				WithReportableDetails(map[string]interface{}{
					"reason":     types.EventDeletionReasonMutationTimeout,
					"request_id": types.GetRequestID(ctx),
				}).
				Mark(ierr.ErrDatabase)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(mutationPollInterval):
		}
	}
}

// selectIDs runs a single-column id query and collects the column.
func (r *EventDeletionLogRepository) selectIDs(ctx context.Context, query string, args []interface{}) ([]string, error) {
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// kill cancels a mutation this request submitted but stopped waiting for. Without
// it an abandoned delete keeps rewriting parts with nothing watching, and a
// second attempt piles another rewrite on top of the first.
func (r *EventDeletionLogRepository) kill(ctx context.Context, table, marker string) {
	killCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), mutationKillTimeout)
	defer cancel()

	query := "KILL MUTATION WHERE database = currentDatabase() AND table = ? AND position(command, ?) > 0"
	if err := r.store.GetConn().Exec(killCtx, query, table, marker); err != nil {
		r.logger.Error(killCtx, "failed to cancel the abandoned delete mutation, it is still running",
			"error", err,
			"table", table,
		)
		return
	}

	r.logger.Info(killCtx, "cancelled the abandoned delete mutation", "table", table)
}
