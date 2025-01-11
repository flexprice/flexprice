package ent

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/ent/tenant"
	domainTenant "github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type tenantRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewTenantRepository(client postgres.IClient, log *logger.Logger) domainTenant.Repository {
	return &tenantRepository{
		client: client,
		log:    log,
	}
}

func (r *tenantRepository) Create(ctx context.Context, tenantEntity *domainTenant.Tenant) error {
	client := r.client.Querier(ctx)
	_, err := client.Tenant.Create().
		SetID(tenantEntity.ID).
		SetName(tenantEntity.Name).
		SetCreatedAt(tenantEntity.CreatedAt).
		SetUpdatedAt(tenantEntity.UpdatedAt).
		Save(ctx)

	if err != nil {
		r.log.Error("failed to create tenant", "error", err)
		return fmt.Errorf("creating tenant: %w", err)
	}

	return nil
}

func (r *tenantRepository) GetByID(ctx context.Context, id string) (*domainTenant.Tenant, error) {
	client := r.client.Querier(ctx)

	tenantEntity, err := client.Tenant.Query().
		Where(tenant.ID(id)).
		Only(ctx)

	if err != nil {
		r.log.Error("failed to get tenant by ID", "error", err)
		return nil, fmt.Errorf("getting tenant by ID: %w", err)
	}

	return domainTenant.FromEnt(tenantEntity), nil
}
