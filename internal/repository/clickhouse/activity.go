package clickhouse

import (
	"context"
	"time"

	clickhouse_go "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/activity"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
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
			actor_type, actor_id, actor_label, source, request_id, ip, user_agent, state, schema_version, occurred_at
		)`)
	if err != nil {
		return ierr.WithError(err).WithHint("Failed to prepare batch for activity_logs insert").Mark(ierr.ErrDatabase)
	}
	for _, l := range logs {
		if err := batch.Append(
			l.ID, l.TenantID, l.EnvironmentID, l.EntityType, l.EntityID, l.EntityLabel, l.CustomerID, l.Action,
			l.ActorType, l.ActorID, l.ActorLabel, l.Source, l.RequestID, l.IP, l.UserAgent, l.State, l.SchemaVersion, l.OccurredAt.UTC(),
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

func (r *activityRepository) List(context.Context, *activity.ListQuery) ([]*activity.Log, error) {
	return nil, errActivityNotImplemented
}

func (r *activityRepository) States(context.Context, []string, time.Time, time.Time) (map[string]string, error) {
	return nil, errActivityNotImplemented
}

func (r *activityRepository) Get(context.Context, string, time.Time, time.Time) (*activity.Log, error) {
	return nil, errActivityNotImplemented
}

var errActivityNotImplemented = ierr.NewError("not implemented").Mark(ierr.ErrInternal)
