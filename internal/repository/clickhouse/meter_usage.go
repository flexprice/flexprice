package clickhouse

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/clickhouse"
	meterusage "github.com/flexprice/flexprice/internal/domain/meter_usage"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/samber/lo"
)

type MeterUsageRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewMeterUsageRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) meterusage.Repository {
	return &MeterUsageRepository{
		store:  store,
		logger: logger,
	}
}

// BulkInsert inserts multiple meter_usage rows into ClickHouse in batches of 100.
// Properties (map) is serialised to JSON here; nil map writes an empty string.
func (r *MeterUsageRepository) BulkInsert(ctx context.Context, records []*meterusage.MeterUsage) error {
	if len(records) == 0 {
		return nil
	}

	batches := lo.Chunk(records, 100)

	for _, batch := range batches {
		b, err := r.store.GetConn().PrepareBatch(ctx, `
			INSERT INTO meter_usage (
				id, tenant_id, environment_id, external_customer_id, meter_id,
				timestamp, ingested_at, qty_total, unique_hash, source, properties
			)
		`)
		if err != nil {
			return ierr.WithError(err).
				WithHint("Failed to prepare batch for meter_usage").
				Mark(ierr.ErrDatabase)
		}

		for _, rec := range batch {
			propertiesJSON := ""
			if len(rec.Properties) > 0 {
				raw, err := json.Marshal(rec.Properties)
				if err != nil {
					return ierr.WithError(err).
						WithHint("Failed to marshal meter_usage properties").
						WithReportableDetails(map[string]interface{}{
							"record_id": rec.ID,
						}).
						Mark(ierr.ErrValidation)
				}
				propertiesJSON = string(raw)
			}

			err = b.Append(
				rec.ID,
				rec.TenantID,
				rec.EnvironmentID,
				rec.ExternalCustomerID,
				rec.MeterID,
				rec.Timestamp,
				rec.IngestedAt,
				rec.QtyTotal,
				rec.UniqueHash,
				rec.Source,
				propertiesJSON,
			)
			if err != nil {
				return ierr.WithError(err).
					WithHint("Failed to append meter_usage row to batch").
					WithReportableDetails(map[string]interface{}{
						"record_id": rec.ID,
					}).
					Mark(ierr.ErrDatabase)
			}
		}

		if err := b.Send(); err != nil {
			return ierr.WithError(err).
				WithHint("Failed to execute batch insert for meter_usage").
				WithReportableDetails(map[string]interface{}{
					"record_count": len(records),
				}).
				Mark(ierr.ErrDatabase)
		}
	}

	return nil
}
