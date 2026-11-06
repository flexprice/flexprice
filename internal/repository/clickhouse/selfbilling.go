package clickhouse

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// selfBillingEventCountQuery counts across every tenant on purpose: Flexprice bills each tenant on its ingest volume.
// Hours are pinned to UTC. The timestamp bound lets partitions prune; later arrivals go uncounted.
const selfBillingEventCountQuery = `
SELECT
	tenant_id,
	environment_id,
	toStartOfHour(ingested_at, 'UTC') AS hour,
	uniqExact(id) AS cnt
FROM events
WHERE ingested_at >= ? AND ingested_at < ?
	AND timestamp >= ?
GROUP BY tenant_id, environment_id, hour
SETTINGS ` + maxMemoryUsageSetting

// selfBillingLateEventHorizon bounds event age, so old partitions prune.
const selfBillingLateEventHorizon = 30 * 24 * time.Hour

type selfBillingRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewSelfBillingRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) domainsb.ClickHouseRepository {
	return &selfBillingRepository{
		store:  store,
		logger: logger,
	}
}

func (r *selfBillingRepository) CountEventsByTenantHour(ctx context.Context, from, to time.Time) ([]*domainsb.TenantHourEvents, error) {
	rows, err := r.store.GetConn().Query(ctx, selfBillingEventCountQuery, from.UTC(), to.UTC(), from.UTC().Add(-selfBillingLateEventHorizon))
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to count events for self-billing").
			Mark(ierr.ErrDatabase)
	}
	defer rows.Close()

	var out []*domainsb.TenantHourEvents
	for rows.Next() {
		row := &domainsb.TenantHourEvents{}
		if err := rows.Scan(&row.TenantID, &row.EnvironmentID, &row.Hour, &row.Count); err != nil {
			return nil, ierr.WithError(err).
				WithHint("Failed to read self-billing event counts").
				Mark(ierr.ErrDatabase)
		}
		row.Hour = row.Hour.UTC()
		out = append(out, row)
	}

	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to read self-billing event counts").
			Mark(ierr.ErrDatabase)
	}
	return out, nil
}
