package clickhouse

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/domain/events"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
)

type UsageBenchmarkRepository struct {
	store  *clickhouse.ClickHouseStore
	logger *logger.Logger
}

func NewUsageBenchmarkRepository(store *clickhouse.ClickHouseStore, logger *logger.Logger) events.UsageBenchmarkRepository {
	return &UsageBenchmarkRepository{store: store, logger: logger}
}

func (r *UsageBenchmarkRepository) Insert(ctx context.Context, record *events.UsageBenchmarkRecord) error {
	if record == nil {
		return ierr.NewError("record is required").Mark(ierr.ErrValidation)
	}

	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	}

	stmt, err := r.store.GetConn().PrepareBatch(ctx, `
		INSERT INTO usage_benchmark (
			tenant_id, environment_id, subscription_id,
			start_time, end_time,
			feature_usage_amount, meter_usage_amount, diff,
			currency, created_at
		)
	`)
	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to prepare usage_benchmark insert").
			Mark(ierr.ErrDatabase)
	}

	if err := stmt.Append(
		record.TenantID,
		record.EnvironmentID,
		record.SubscriptionID,
		record.StartTime,
		record.EndTime,
		record.FeatureUsageAmount,
		record.MeterUsageAmount,
		record.Diff,
		record.Currency,
		record.CreatedAt,
	); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to append usage_benchmark row").
			Mark(ierr.ErrDatabase)
	}

	if err := stmt.Send(); err != nil {
		return ierr.WithError(err).
			WithHint("Failed to send usage_benchmark insert").
			Mark(ierr.ErrDatabase)
	}

	r.logger.Debugw("inserted usage_benchmark row",
		"subscription_id", record.SubscriptionID,
		"tenant_id", record.TenantID,
	)

	return nil
}
