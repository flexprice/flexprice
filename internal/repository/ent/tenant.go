package ent

import (
	"context"
	"strings"

	"github.com/flexprice/flexprice/ent"
	entTenant "github.com/flexprice/flexprice/ent/tenant"
	domainTenant "github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
)

type tenantRepository struct {
	client postgres.IClient
	logger *logger.Logger
}

// NewTenantRepository creates a new tenant repository
func NewTenantRepository(client postgres.IClient, logger *logger.Logger) domainTenant.Repository {
	return &tenantRepository{
		client: client,
		logger: logger,
	}
}

// Create creates a new tenant
func (r *tenantRepository) Create(ctx context.Context, tenant *domainTenant.Tenant) error {
	r.logger.Debugw("creating tenant", "tenant_id", tenant.ID, "name", tenant.Name)

	addressLines := strings.Split(tenant.TenantBillingInfo.Address.Street, "\n")
	var addressLine1, addressLine2 string
	if len(addressLines) > 0 {
		addressLine1 = addressLines[0]
	}
	if len(addressLines) > 1 {
		addressLine2 = strings.Join(addressLines[1:], "\n")
	}

	client := r.client.Querier(ctx)
	_, err := client.Tenant.
		Create().
		SetID(tenant.ID).
		SetName(tenant.Name).
		SetStatus(string(tenant.Status)).
		SetCreatedAt(tenant.CreatedAt).
		SetUpdatedAt(tenant.UpdatedAt).
		SetBillingInfo(map[string]interface{}{
			"address": map[string]interface{}{
				"address_line_1": addressLine1,
				"address_line_2": addressLine2,
				"city":           tenant.TenantBillingInfo.Address.City,
				"state":          tenant.TenantBillingInfo.Address.State,
				"postal_code":    tenant.TenantBillingInfo.Address.PostalCode,
			},
			"email":      tenant.TenantBillingInfo.Email,
			"website":    tenant.TenantBillingInfo.Website,
			"help_email": tenant.TenantBillingInfo.HelpEmail,
		}).
		Save(ctx)

	if err != nil {
		return ierr.WithError(err).
			WithHint("Failed to create tenant").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": tenant.ID,
				"name":      tenant.Name,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

// GetByID retrieves a tenant by ID
func (r *tenantRepository) GetByID(ctx context.Context, id string) (*domainTenant.Tenant, error) {
	client := r.client.Querier(ctx)
	tenant, err := client.Tenant.
		Query().
		Where(
			entTenant.ID(id),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Tenant not found").
				WithReportableDetails(map[string]interface{}{
					"tenant_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to retrieve tenant").
			WithReportableDetails(map[string]interface{}{
				"tenant_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainTenant.FromEnt(tenant), nil
}

// List retrieves all tenants
func (r *tenantRepository) List(ctx context.Context) ([]*domainTenant.Tenant, error) {
	client := r.client.Querier(ctx)
	tenants, err := client.Tenant.
		Query().
		Order(ent.Desc(entTenant.FieldCreatedAt)).
		All(ctx)

	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list tenants").
			Mark(ierr.ErrDatabase)
	}

	return domainTenant.FromEntList(tenants), nil
}
