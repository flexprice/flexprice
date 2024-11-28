package postgres

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/meter"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type MeterRepository interface {
	CreateMeter(ctx context.Context, meter *meter.Meter) error
	GetMeter(ctx context.Context, id string) (*meter.Meter, error)
	GetAllMeters(ctx context.Context) ([]*meter.Meter, error)
	DisableMeter(ctx context.Context, id string) error
}

type repository struct {
	db     *postgres.DB
	logger *logger.Logger
}

func NewMeterRepository(db *postgres.DB, logger *logger.Logger) MeterRepository {
	return &repository{db: db, logger: logger}
}

func (r *repository) CreateMeter(ctx context.Context, meter *meter.Meter) error {
	aggregationJSON, err := json.Marshal(meter.Aggregation)
	if err != nil {
		return fmt.Errorf("marshal aggregation: %w", err)
	}

	query := `
	INSERT INTO meters (
		id, tenant_id, event_name, aggregation, window_size, 
		created_at, updated_at, created_by, updated_by, status
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10
	)
	`

	_, err = r.db.ExecContext(ctx, query,
		meter.ID,
		meter.TenantID,
		meter.EventName,
		aggregationJSON,
		meter.WindowSize,
		meter.CreatedAt,
		meter.UpdatedAt,
		meter.CreatedBy,
		meter.UpdatedBy,
		meter.Status,
	)

	if err != nil {
		return fmt.Errorf("insert meter: %w", err)
	}

	return nil
}

func (r *repository) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	meter := &meter.Meter{}
	var aggregationJSON []byte

	query := `
	SELECT 
		id, tenant_id, event_name, aggregation, window_size, 
		created_at, updated_at, created_by, updated_by, status
	FROM meters
	WHERE id = $1
	`

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&meter.ID,
		&meter.TenantID,
		&meter.EventName,
		&aggregationJSON,
		&meter.WindowSize,
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

func (r *repository) GetAllMeters(ctx context.Context) ([]*meter.Meter, error) {
	query := `
	SELECT 
		id, tenant_id, event_name, aggregation, window_size, 
		created_at, updated_at, created_by, updated_by, status
	FROM meters
	WHERE status = 'active'
	ORDER BY created_at DESC
	`

	rows, err := r.db.QueryContext(ctx, query)
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
			&meter.WindowSize,
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

func (r *repository) DisableMeter(ctx context.Context, id string) error {
	query := `
		UPDATE meters 
		SET status = 'disabled', updated_at = NOW()
		WHERE id = $1 AND status = 'active'
	`

	result, err := r.db.ExecContext(ctx, query, id)
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
