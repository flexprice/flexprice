package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type planRepository struct {
	db     *postgres.DB
	logger *logger.Logger
}

func NewPlanRepository(db *postgres.DB, logger *logger.Logger) plan.Repository {
	return &planRepository{db: db, logger: logger}
}

func (r *planRepository) Create(ctx context.Context, plan *plan.Plan) error {
	query := `
		INSERT INTO plans (
			id, 
			tenant_id, 
			lookup_key, 
			name, 
			description, 
			invoice_cadence, 
			trial_period, 
			status, 
			created_at, 
			updated_at, 
			created_by, 
			updated_by
		)
		VALUES (
			:id, 
			:tenant_id, 
			:lookup_key, 
			:name, 
			:description, 
			:invoice_cadence, 
			:trial_period, 
			:status, 
			:created_at, 
			:updated_at, 
			:created_by, 
			:updated_by
		)
	`

	r.logger.Debug("creating plan ",
		"plan_id", plan.ID,
		"tenant_id", plan.TenantID,
	)

	_, err := r.db.NamedExecContext(ctx, query, plan)
	if err != nil {
		r.logger.Error("failed to create plan", "error", err)
		return fmt.Errorf("failed to insert plan: %w", err)
	}

	return nil
}

func (r *planRepository) Get(ctx context.Context, id string) (*plan.Plan, error) {
	query := `
		SELECT * FROM plans 
		WHERE id = :id 
		AND tenant_id = :tenant_id
	`

	var p plan.Plan
	rows, err := r.db.NamedQueryContext(ctx, query, map[string]interface{}{
		"id":        id,
		"tenant_id": types.GetTenantID(ctx),
	})
	if err != nil {
		r.logger.Error("failed to get plan", "error", err)
		return nil, err
	}

	defer rows.Close()

	if !rows.Next() {
		return nil, fmt.Errorf("plan not found")
	}

	if err := rows.StructScan(&p); err != nil {
		return nil, err
	}

	return &p, nil
}

func (r *planRepository) List(ctx context.Context, filter types.Filter) ([]*plan.Plan, error) {
	query := `
		SELECT * FROM plans 
		WHERE tenant_id = :tenant_id 
		AND status = :status
		ORDER BY created_at DESC 
		LIMIT :limit OFFSET :offset
	`

	var plans []*plan.Plan
	rows, err := r.db.NamedQueryContext(ctx, query, map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"status":    types.StatusPublished,
		"limit":     filter.Limit,
		"offset":    filter.Offset,
	})
	if err != nil {
		r.logger.Error("failed to list plans", "error", err)
		return nil, err
	}

	defer rows.Close()

	for rows.Next() {
		var p plan.Plan
		if err := rows.StructScan(&p); err != nil {
			return nil, err
		}
		plans = append(plans, &p)
	}

	return plans, nil
}

func (r *planRepository) Update(ctx context.Context, plan *plan.Plan) error {
	query := `
		UPDATE plans 
		SET lookup_key = :lookup_key, 
		name = :name, 
		description = :description, 
		invoice_cadence = :invoice_cadence, 
		trial_period = :trial_period, 
		updated_at = :updated_at, 
		updated_by = :updated_by 
		WHERE id = :id 
		AND tenant_id = :tenant_id
	`

	r.logger.Debug("updating plan",
		"plan_id", plan.ID,
		"tenant_id", plan.TenantID,
	)

	_, err := r.db.NamedExecContext(ctx, query, plan)
	if err != nil {
		r.logger.Error("failed to update plan", "error", err)
		return err
	}
	return nil
}

func (r *planRepository) Delete(ctx context.Context, id string) error {
	query := `
		UPDATE plans 
		SET status = :status, 
		updated_at = :updated_at, 
		updated_by = :updated_by 
		WHERE id = :id
		AND tenant_id = :tenant_id
	`

	r.logger.Debug("deleting plan",
		"plan_id", id,
	)

	_, err := r.db.NamedExecContext(ctx, query, map[string]interface{}{
		"id":         id,
		"status":     types.StatusDeleted,
		"updated_at": time.Now().UTC(),
		"updated_by": types.GetUserID(ctx),
		"tenant_id":  types.GetTenantID(ctx),
	})

	if err != nil {
		r.logger.Error("failed to delete plan", "error", err)
		return err
	}
	return nil
}
