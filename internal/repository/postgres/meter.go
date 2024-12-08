package postgres

import (
	"context"
	"encoding/json"
	"fmt"

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
	query := `
	INSERT INTO meters (
		id, tenant_id, name, event_name, filters, aggregation, reset_usage,
		created_at, updated_at, created_by, updated_by, status
	) VALUES (
		$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12
	)
	`

	aggregationJSON, err := json.Marshal(meter.Aggregation)
	if err != nil {
		return fmt.Errorf("marshal aggregation: %w", err)
	}

	filtersJSON, err := json.Marshal(meter.Filters)
	if err != nil {
		return fmt.Errorf("marshal filters: %w", err)
	}

	_, err = r.db.ExecContext(ctx, query,
		meter.ID,
		meter.TenantID,
		meter.Name,
		meter.EventName,
		filtersJSON,
		aggregationJSON,
		meter.ResetUsage,
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

func (r *meterRepository) GetMeter(ctx context.Context, id string) (*meter.Meter, error) {
	query := `
	SELECT 
		id, tenant_id, name, event_name, filters, aggregation, reset_usage,
		created_at, updated_at, created_by, updated_by, status
	FROM meters 
	WHERE id = $1 AND status = $2
	`

	var m meter.Meter
	var filtersJSON, aggregationJSON []byte

	err := r.db.QueryRowContext(ctx, query, id, types.StatusActive).Scan(
		&m.ID,
		&m.TenantID,
		&m.Name,
		&m.EventName,
		&filtersJSON,
		&aggregationJSON,
		&m.ResetUsage,
		&m.CreatedAt,
		&m.UpdatedAt,
		&m.CreatedBy,
		&m.UpdatedBy,
		&m.Status,
	)

	if err != nil {
		return nil, fmt.Errorf("get meter: %w", err)
	}

	// Unmarshal filters
	if len(filtersJSON) > 0 {
		if err := json.Unmarshal(filtersJSON, &m.Filters); err != nil {
			return nil, fmt.Errorf("unmarshal filters: %w", err)
		}
	}

	// Unmarshal aggregation
	if len(aggregationJSON) > 0 {
		if err := json.Unmarshal(aggregationJSON, &m.Aggregation); err != nil {
			return nil, fmt.Errorf("unmarshal aggregation: %w", err)
		}
	}

	return &m, nil
}

func (r *meterRepository) GetAllMeters(ctx context.Context) ([]*meter.Meter, error) {
	query := `
	SELECT 
		id, tenant_id, name, event_name, filters, aggregation, reset_usage,
		created_at, updated_at, created_by, updated_by, status
	FROM meters 
	WHERE status = $1
	`

	rows, err := r.db.QueryContext(ctx, query, types.StatusActive)
	if err != nil {
		return nil, fmt.Errorf("query meters: %w", err)
	}
	defer rows.Close()

	var meters []*meter.Meter
	for rows.Next() {
		var m meter.Meter
		var filtersJSON, aggregationJSON []byte

		err := rows.Scan(
			&m.ID,
			&m.TenantID,
			&m.Name,
			&m.EventName,
			&filtersJSON,
			&aggregationJSON,
			&m.ResetUsage,
			&m.CreatedAt,
			&m.UpdatedAt,
			&m.CreatedBy,
			&m.UpdatedBy,
			&m.Status,
		)
		if err != nil {
			return nil, fmt.Errorf("scan meter: %w", err)
		}

		// Unmarshal filters
		if len(filtersJSON) > 0 {
			if err := json.Unmarshal(filtersJSON, &m.Filters); err != nil {
				return nil, fmt.Errorf("unmarshal filters: %w", err)
			}
		}

		// Unmarshal aggregation
		if len(aggregationJSON) > 0 {
			if err := json.Unmarshal(aggregationJSON, &m.Aggregation); err != nil {
				return nil, fmt.Errorf("unmarshal aggregation: %w", err)
			}
		}

		meters = append(meters, &m)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate meters: %w", err)
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
