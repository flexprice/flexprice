package clickhouse

import (
	"context"
	"slices"
	"strings"
	"time"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/activity"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/types"
)

type activityRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewActivityRepository(store *clickhouse.ClickHouseStore, log *logger.Logger) activity.Repository {
	return &activityRepository{store: store, logger: log}
}

// Insert writes all logs in one batch and waits for the async insert to be flushed.
func (r *activityRepository) Insert(ctx context.Context, logs []*activity.Log) error {
	if len(logs) == 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, insertTimeout)
	defer cancel()
	ctx = clickhouse_go.Context(ctx, clickhouse_go.WithSettings(clickhouse_go.Settings{
		"async_insert":          1,
		"wait_for_async_insert": 1,
	}))

	batch, err := r.store.GetConn().PrepareBatch(ctx, `
		INSERT INTO activity_logs (
			id, tenant_id, environment_id, entity_type, entity_id, entity_label, customer_id, action,
			actor_type, actor_id, source, request_id, ip, user_agent, state, schema_version, occurred_at
		)`)
	if err != nil {
		return ierr.WithError(err).WithHint("Failed to prepare batch for activity_logs insert").Mark(ierr.ErrDatabase)
	}
	for _, l := range logs {
		if err := batch.Append(
			l.ID, l.TenantID, l.EnvironmentID, l.Entity.Type, l.Entity.ID, l.Entity.Label, l.CustomerID, l.Action,
			l.Actor.Type, l.Actor.ID, l.Request.Source, l.Request.ID, l.Request.IP, l.Request.UserAgent, l.State, l.SchemaVersion, l.OccurredAt.UTC(),
		); err != nil {
			return ierr.WithError(err).WithHint("Failed to append row to activity_logs batch").
				WithReportableDetails(map[string]interface{}{"activity_id": l.ID}).Mark(ierr.ErrDatabase)
		}
	}
	if err := batch.Send(); err != nil {
		return ierr.WithError(err).WithHint("Failed to send activity_logs batch").Mark(ierr.ErrDatabase)
	}
	return nil
}

// millisParam binds a time as epoch milliseconds; the driver's time.Time binding drops sub-second precision.
const millisParam = "fromUnixTimestamp64Milli(?)"

const activityListColumns = `id, entity_type, entity_id, entity_label, customer_id, action,
	actor_type, actor_id, source, request_id, ip, user_agent, schema_version, occurred_at`

// List returns up to q.Limit entries, newest first, without state.
func (r *activityRepository) List(ctx context.Context, q *activity.ListQuery) ([]*activity.Log, error) {
	where := []string{"tenant_id = ?", "environment_id = ?", "occurred_at >= " + millisParam, "occurred_at < " + millisParam}
	args := []any{types.GetTenantID(ctx), types.GetEnvironmentID(ctx), q.From.UnixMilli(), q.To.UnixMilli()}
	eq := func(column, value string) {
		if value != "" {
			where = append(where, column+" = ?")
			args = append(args, value)
		}
	}
	in := func(column string, values []string) {
		if len(values) > 0 {
			where = append(where, column+" IN (?)")
			args = append(args, values)
		}
	}
	eq("entity_type", q.EntityType)
	eq("entity_id", q.EntityID)
	eq("customer_id", q.CustomerID)
	eq("actor_id", q.ActorID)
	eq("request_id", q.RequestID)
	in("actor_type", q.ActorTypes)
	in("action", q.Actions)

	return uniqueEntries(q.Limit, q.After, func(after *activity.Cursor) ([]*activity.Log, error) {
		return r.listBatch(ctx, where, args, after, q.Limit)
	})
}

// uniqueEntries reads batches until it has limit distinct entries or the rows run out.
// Unmerged copies of an entry share (occurred_at, id), so they arrive next to each other.
func uniqueEntries(limit int, after *activity.Cursor, fetch func(after *activity.Cursor) ([]*activity.Log, error)) ([]*activity.Log, error) {
	var logs []*activity.Log
	for {
		batch, err := fetch(after)
		if err != nil {
			return nil, err
		}
		for _, l := range batch {
			if n := len(logs); n > 0 && logs[n-1].ID == l.ID {
				continue
			}
			logs = append(logs, l)
		}
		if len(batch) < limit || len(logs) >= limit {
			return logs[:min(len(logs), limit)], nil
		}
		last := batch[len(batch)-1]
		after = &activity.Cursor{OccurredAt: last.OccurredAt, ID: last.ID}
	}
}

// listBatch reads up to limit rows after the cursor, newest first. A plain LIMIT lets the in-order read stop early.
func (r *activityRepository) listBatch(ctx context.Context, where []string, args []any, after *activity.Cursor, limit int) ([]*activity.Log, error) {
	where, args = slices.Clip(where), slices.Clip(args)
	if after != nil {
		where = append(where, "(occurred_at, id) < ("+millisParam+", ?)")
		args = append(args, after.OccurredAt.UnixMilli(), after.ID)
	}
	args = append(args, limit)

	query := "SELECT " + activityListColumns + " FROM activity_logs WHERE " + strings.Join(where, " AND ") +
		" ORDER BY occurred_at DESC, id DESC LIMIT ?"
	rows, err := r.store.GetConn().Query(ctx, query, args...)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to list activity").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var logs []*activity.Log
	for rows.Next() {
		l := &activity.Log{}
		if err := rows.Scan(&l.ID, &l.Entity.Type, &l.Entity.ID, &l.Entity.Label, &l.CustomerID, &l.Action,
			&l.Actor.Type, &l.Actor.ID, &l.Request.Source, &l.Request.ID, &l.Request.IP, &l.Request.UserAgent, &l.SchemaVersion, &l.OccurredAt); err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to read activity").Mark(ierr.ErrDatabase)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read activity").Mark(ierr.ErrDatabase)
	}
	return logs, nil
}

// States returns the stored state for each id; entries stored without state map to "".
func (r *activityRepository) States(ctx context.Context, ids []string, from, to time.Time) (map[string]string, error) {
	rows, err := r.store.GetConn().Query(ctx, `
		SELECT id, state FROM activity_logs
		WHERE tenant_id = ? AND environment_id = ? AND occurred_at >= `+millisParam+` AND occurred_at <= `+millisParam+` AND id IN (?)
		LIMIT 1 BY id`,
		types.GetTenantID(ctx), types.GetEnvironmentID(ctx), from.UnixMilli(), to.UnixMilli(), ids)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read activity state").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	states := make(map[string]string, len(ids))
	for rows.Next() {
		var id, state string
		if err := rows.Scan(&id, &state); err != nil {
			return nil, ierr.WithError(err).WithHint("Failed to read activity state").Mark(ierr.ErrDatabase)
		}
		states[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read activity state").Mark(ierr.ErrDatabase)
	}
	return states, nil
}

// Get returns one entry with its state, or nil when no row matches.
func (r *activityRepository) Get(ctx context.Context, id string, from, to time.Time) (*activity.Log, error) {
	rows, err := r.store.GetConn().Query(ctx, "SELECT "+activityListColumns+`, state FROM activity_logs
		WHERE tenant_id = ? AND environment_id = ? AND occurred_at >= `+millisParam+` AND occurred_at <= `+millisParam+` AND id = ?
		LIMIT 1`,
		types.GetTenantID(ctx), types.GetEnvironmentID(ctx), from.UnixMilli(), to.UnixMilli(), id)
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to get activity").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, rows.Err()
	}
	l := &activity.Log{}
	if err := rows.Scan(&l.ID, &l.Entity.Type, &l.Entity.ID, &l.Entity.Label, &l.CustomerID, &l.Action,
		&l.Actor.Type, &l.Actor.ID, &l.Request.Source, &l.Request.ID, &l.Request.IP, &l.Request.UserAgent, &l.SchemaVersion, &l.OccurredAt, &l.State); err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read activity").Mark(ierr.ErrDatabase)
	}
	return l, nil
}
