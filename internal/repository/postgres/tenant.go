package postgres

import (
	"context"
	"database/sql"

	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
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

func (r *tenantRepository) Create(ctx context.Context, t *tenant.Tenant) error {
	query := `
        SELECT 1 FROM tenants where name = $1
    `
	var exists bool
	err := r.db.QueryRowContext(
		ctx,
		query,
		t.Name,
	).Scan(&exists)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	if exists {
		return ierr.New(ierr.ErrAlreadyExists, "tenant already exists")
	}
	query = `
	INSERT INTO tenants (id, name, created_at, updated_at)
	VALUES ($1, $2, $3, $4)
	`
	_, err = r.db.ExecContext(
		ctx, query,
		t.ID,
		t.Name,
		t.CreatedAt,
		t.UpdatedAt,
	)
	return err
}

func (r *tenantRepository) GetByID(ctx context.Context, id string) (*tenant.Tenant, error) {
	query := `SELECT * FROM tenants WHERE id = $1`
	var t tenant.Tenant
	err := r.db.GetContext(ctx, &t, query, id)
	if err == sql.ErrNoRows {
		return nil, ierr.Newf(ierr.ErrNotFound, "tenant not found with if:%s", id)
	}
	return &t, err
}
