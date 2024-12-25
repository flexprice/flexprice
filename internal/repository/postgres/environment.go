package postgres

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/environment"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type environmentRepository struct {
	db     *postgres.DB
	logger *logger.Logger
}

func NewEnvironmentRepository(db *postgres.DB, logger *logger.Logger) environment.Repository {
	return &environmentRepository{db: db, logger: logger}
}

func (r *environmentRepository) Create(ctx context.Context, env *environment.Environment) error {
	query := `
		INSERT INTO environments (
			id, tenant_id, name, type, slug, status, created_at, updated_at, created_by, updated_by
		) VALUES (
			:id, :tenant_id, :name, :type, :slug, :status, :created_at, :updated_at, :created_by, :updated_by
		)`

	r.logger.Debug("creating environment", "environment_id", env.ID, "tenant_id", env.TenantID)

	_, err := r.db.NamedExecContext(ctx, query, env)
	return err
}

func (r *environmentRepository) Get(ctx context.Context, id string) (*environment.Environment, error) {
	var env environment.Environment
	query := `SELECT * FROM environments WHERE id = :id AND tenant_id = :tenant_id`

	tenantID := ctx.Value(types.CtxTenantID)
	params := map[string]interface{}{
		"id":        id,
		"tenant_id": tenantID,
	}

	err := r.db.GetContext(ctx, &env, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to get environment: %w", err)
	}
	return &env, nil
}

func (r *environmentRepository) List(ctx context.Context, filter types.Filter) ([]*environment.Environment, error) {
	var environments []*environment.Environment
	query := `
		SELECT * FROM environments 
		WHERE tenant_id = :tenant_id 
		ORDER BY created_at DESC 
		LIMIT :limit OFFSET :offset`

	tenantID := ctx.Value(types.CtxTenantID)
	params := map[string]interface{}{
		"tenant_id": tenantID,
		"limit":     filter.Limit,
		"offset":    filter.Offset,
	}

	rows, err := r.db.NamedQueryContext(ctx, query, params)
	if err != nil {
		return nil, fmt.Errorf("failed to list environments: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var env environment.Environment
		if err := rows.StructScan(&env); err != nil {
			return nil, fmt.Errorf("failed to scan environment: %w", err)
		}
		environments = append(environments, &env)
	}

	return environments, nil
}

func (r *environmentRepository) Update(ctx context.Context, env *environment.Environment) error {
	query := `
		UPDATE environments SET
			name = :name,
			type = :type,
			slug = :slug,
			updated_at = :updated_at,
			updated_by = :updated_by
		WHERE id = :id AND tenant_id = :tenant_id`

	r.logger.Debug("updating environment", "environment_id", env.ID, "tenant_id", env.TenantID)

	params := map[string]interface{}{
		"id":         env.ID,
		"tenant_id":  env.TenantID,
		"name":       env.Name,
		"type":       env.Type,
		"slug":       env.Slug,
		"updated_at": env.UpdatedAt,
		"updated_by": env.UpdatedBy,
	}

	_, err := r.db.NamedExecContext(ctx, query, params)
	return err
}
