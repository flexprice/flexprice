package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/flexprice/flexprice/internal/database/builder"
	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type meterRepository struct {
	db     *postgres.DB
	logger *logger.Logger
}

func NewMeterRepository(db *postgres.DB, logger *logger.Logger) meter.Repository {
	return &meterRepository{db: db, logger: logger}
}

func (r *meterRepository) CreateMeter(ctx context.Context, meter *meter.Meter) error {
	aggregationJSON, err := json.Marshal(meter.Aggregation)
	if err != nil {
		return fmt.Errorf("marshal aggregation: %w", err)
	}

	query, args, err := builder.New().
		Insert("meters").
		Columns("id", "event_name", "aggregation", "created_at", "updated_at", "created_by", "updated_by", "status").
		Values(
			meter.ID,
			meter.EventName,
			aggregationJSON,
			meter.CreatedAt,
			meter.UpdatedAt,
			meter.CreatedBy,
			meter.UpdatedBy,
			meter.Status,
		).
		WithContext(ctx).
		ToSql()

	if err != nil {
		return fmt.Errorf("build query: %w", err)
	}

	_, err = r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("insert meter: %w", err)
	}

	return nil
}

func (r *meterRepository) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	query, args, err := builder.New().
		Select("id", "tenant_id", "event_name", "aggregation",
			"created_at", "updated_at", "created_by", "updated_by", "status").
		From("meters").
		Where(sq.Eq{"id": id}).
		WithContext(ctx).
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	meter := &meter.Meter{}
	var aggregationJSON []byte

	err = r.db.QueryRowContext(ctx, query, args...).Scan(
		&meter.ID,
		&meter.TenantID,
		&meter.EventName,
		&aggregationJSON,
		&meter.CreatedAt,
		&meter.UpdatedAt,
		&meter.CreatedBy,
		&meter.UpdatedBy,
		&meter.Status,
	)

	if err != nil {
		return nil, fmt.Errorf("get meter: %w", err)
	}

	if err := json.Unmarshal(aggregationJSON, &meter.Aggregation); err != nil {
		return nil, fmt.Errorf("unmarshal aggregation: %w", err)
	}

	return meter, nil
}

func (r *meterRepository) GetAllMeters(ctx context.Context) ([]*meter.Meter, error) {
	query, args, err := builder.New().
		Select("id", "tenant_id", "event_name", "aggregation",
			"created_at", "updated_at", "created_by", "updated_by", "status").
		From("meters").
		Where(sq.Eq{"status": types.StatusActive}).
		WithContext(ctx).
		OrderBy("created_at DESC").
		ToSql()

	if err != nil {
		return nil, fmt.Errorf("build query: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query meters: %w", err)
	}
	defer rows.Close()

	var meters []*meter.Meter
	for rows.Next() {
		var meter meter.Meter
		var aggregationJSON []byte

		err := rows.Scan(
			&meter.ID,
			&meter.TenantID,
			&meter.EventName,
			&aggregationJSON,
			&meter.CreatedAt,
			&meter.UpdatedAt,
			&meter.CreatedBy,
			&meter.UpdatedBy,
			&meter.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("scan meter: %w", err)
		}

		if err := json.Unmarshal(aggregationJSON, &meter.Aggregation); err != nil {
			return nil, fmt.Errorf("unmarshal aggregation: %w", err)
		}

		meters = append(meters, &meter)
	}

	return meters, nil
}

func (r *meterRepository) DisableMeter(ctx context.Context, id string) error {
	query := `
		UPDATE meters 
		SET status = 'disabled', updated_at = NOW(), updated_by = $1
		WHERE id = $2 AND tenant_id = $3 AND status = 'active'
	`

	result, err := r.db.ExecContext(ctx, query, types.GetUserID(ctx), id, types.GetTenantID(ctx))
	if err != nil {
		return fmt.Errorf("disable meter: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}

	if rows == 0 {
		return fmt.Errorf("meter not found or already disabled")
	}

	return nil
}
