package clickhouse

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	domainsb "github.com/flexprice/flexprice/internal/domain/selfbilling"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

// Counts across every tenant on purpose: Flexprice bills each tenant on its ingest volume.
// Filters on ingested_at only; the mm_ingested_at skip index keeps the scan to recent granules.
// Hours are pinned to UTC so buckets do not depend on the server timezone.
const selfBillingCountQuery = `
SELECT tenant_id, environment_id, toStartOfHour(ingested_at, 'UTC') AS hour, uniqExact(id) AS cnt
FROM events
WHERE ingested_at >= ? AND ingested_at < ?
GROUP BY tenant_id, environment_id, hour`

type selfBillingEventCountRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewSelfBillingEventCountRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) domainsb.EventCountRepository {
	return &selfBillingEventCountRepository{store: store, logger: logger}
}

func (r *selfBillingEventCountRepository) CountByTenantHour(ctx context.Context, from, to time.Time) ([]*domainsb.TenantEnvHourCount, error) {
	rows, err := r.store.GetConn().Query(ctx, selfBillingCountQuery, from.UTC(), to.UTC())
	if err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to count events for self-billing").Mark(ierr.ErrDatabase)
	}
	defer rows.Close()
	var out []*domainsb.TenantEnvHourCount
	for rows.Next() {
		c := &domainsb.TenantEnvHourCount{}
		if err := rows.Scan(&c.TenantID, &c.EnvironmentID, &c.Hour, &c.Count); err != nil {
			return nil, ierr.WithError(err).Mark(ierr.ErrDatabase)
		}
		c.Hour = c.Hour.UTC()
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, ierr.WithError(err).WithHint("Failed to read self-billing event counts").Mark(ierr.ErrDatabase)
	}
	return out, nil
}
