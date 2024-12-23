package postgres

import (
	"context"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type tenantRepository struct {
	db     *postgres.DB
	logger *logger.Logger
}

func NewTenantRepository(db *postgres.DB, logger *logger.Logger) tenant.Repository {
	return &tenantRepository{db: db, logger: logger}
}

func (r *tenantRepository) Create(ctx context.Context, tenant *tenant.Tenant) error {
	query := `
	INSERT INTO tenants (id, name, created_at, updated_at)
	VALUES ($1, $2, $3, $4)
	`

	_, err := r.db.ExecContext(
		ctx, query,
		tenant.ID,
		tenant.Name,
		tenant.CreatedAt,
		tenant.UpdatedAt,
	)
	return err
}

func (r *tenantRepository) GetByID(ctx context.Context, id string) (*tenant.Tenant, error) {
	query := `SELECT * FROM tenants WHERE id = $1`

	var tenant tenant.Tenant
	err := r.db.GetContext(ctx, &tenant, query, id)
	return &tenant, err
}
