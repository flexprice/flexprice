package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type priceRepository struct {
	db     *postgres.DB
	logger *logger.Logger
}

func NewPriceRepository(db *postgres.DB, logger *logger.Logger) price.Repository {
	return &priceRepository{db: db, logger: logger}
}

func (r *priceRepository) Create(ctx context.Context, price *price.Price) error {
	price.DisplayAmount = price.GetDisplayAmount()
	query := `
		INSERT INTO prices (
			id, tenant_id, amount, display_amount, currency, plan_id, type, 
			billing_period, billing_period_count, billing_model, billing_cadence, 
			tier_mode, tiers, meter_id, filter_values, transform_quantity, lookup_key, description,
			metadata, status, created_at, updated_at, created_by, updated_by
		) VALUES (
			:id, :tenant_id, :amount, :display_amount, :currency, :plan_id, :type,
			:billing_period, :billing_period_count, :billing_model, :billing_cadence,
			:tier_mode, :tiers, :meter_id, :filter_values, :transform_quantity, :lookup_key,
			:description, :metadata, :status, :created_at, :updated_at, :created_by, :updated_by
		)`

	r.logger.Debug("creating price ",
		"price_id ", price.ID,
		"tenant_id ", price.TenantID,
	)

	_, err := r.db.NamedExecContext(ctx, query, price)
	if err != nil {
		return fmt.Errorf("failed to insert price: %w", err)
	}

	return nil
}

func (r *priceRepository) Get(ctx context.Context, id string) (*price.Price, error) {
	var p price.Price
	query := `
		SELECT * FROM prices 
		WHERE id = :id 
		AND tenant_id = :tenant_id
		AND status = :status`

	rows, err := r.db.NamedQueryContext(ctx, query, map[string]interface{}{
		"id":        id,
		"tenant_id": types.GetTenantID(ctx),
		"status":    types.StatusPublished,
	})

	// TODO: Handle not found error better to not throw 500
	if err != nil {
		return nil, fmt.Errorf("failed to get price: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("price not found")
	}

	if err := rows.StructScan(&p); err != nil {
		return nil, fmt.Errorf("failed to scan price: %w", err)
	}
	return &p, nil
}

func (r *priceRepository) List(ctx context.Context, filter types.PriceFilter) ([]*price.Price, error) {
	var prices []*price.Price

	// Build base query
	query := `
		SELECT * FROM prices
		WHERE tenant_id = :tenant_id`

	// Build params map
	params := map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
	}

	// Add status filter if present
	if filter.Status != nil {
		query += " AND status = :status"
		params["status"] = *filter.Status
	}

	// Add plan IDs filter if present
	if len(filter.PlanIDs) > 0 {
		query += " AND plan_id = ANY(string_to_array(:plan_ids, ','))"
		params["plan_ids"] = strings.Join(filter.PlanIDs, ",")
	}

	// Add ordering
	if filter.Sort != nil {
		query += fmt.Sprintf(" ORDER BY %s", filter.GetSort())
		if filter.Order != nil {
			query += fmt.Sprintf(" %s", filter.GetOrder())
		}
	} else {
		query += " ORDER BY created_at DESC"
	}

	// Add pagination if limits are set
	if filter.Limit != nil {
		query += " LIMIT :limit"
		params["limit"] = filter.GetLimit()
	}

	if filter.Offset != nil {
		query += " OFFSET :offset"
		params["offset"] = filter.GetOffset()
	}

	r.logger.Debugw("listing prices",
		"plan_ids", filter.PlanIDs,
		"tenant_id", types.GetTenantID(ctx),
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset(),
		"status", filter.GetStatus(),
		"query", query,
		"params", params)

	// Execute query
	rows, err := r.db.NamedQueryContext(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list prices: %w", err)
	}
	defer rows.Close()

	// Scan rows
	for rows.Next() {
		var p price.Price
		if err := rows.StructScan(&p); err != nil {
			return nil, fmt.Errorf("failed to scan price: %w", err)
		}
		prices = append(prices, &p)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating price rows: %w", err)
	}

	return prices, nil
}

func (r *priceRepository) Update(ctx context.Context, price *price.Price) error {
	price.UpdatedAt = time.Now().UTC()
	price.UpdatedBy = types.GetUserID(ctx)

	query := `
		UPDATE prices SET 
			lookup_key = :lookup_key,
			description = :description,
			metadata = :metadata,
			status = :status,
			updated_at = :updated_at,
			updated_by = :updated_by
		WHERE id = :id 
		AND tenant_id = :tenant_id`

	result, err := r.db.NamedExecContext(ctx, query, price)
	if err != nil {
		return fmt.Errorf("failed to update price: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return fmt.Errorf("price not found")
	}
	return nil
}

func (r *priceRepository) Delete(ctx context.Context, id string) error {
	query := `
		UPDATE prices SET 
			status = :status,
			updated_at = :updated_at,
			updated_by = :updated_by
		WHERE id = :id 
		AND tenant_id = :tenant_id`

	_, err := r.db.NamedExecContext(ctx, query, map[string]interface{}{
		"id":         id,
		"tenant_id":  types.GetTenantID(ctx),
		"status":     types.StatusDeleted,
		"updated_at": time.Now(),
		"updated_by": types.GetUserID(ctx),
	})
	if err != nil {
		return fmt.Errorf("failed to update price status: %w", err)
	}
	return nil
}
