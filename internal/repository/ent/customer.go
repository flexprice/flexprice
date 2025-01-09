package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/customer"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type customerRepository struct {
	client postgres.IClient
	log    *logger.Logger
}

func NewCustomerRepository(client postgres.IClient, log *logger.Logger) domainCustomer.Repository {
	return &customerRepository{
		client: client,
		log:    log,
	}
}

func (r *customerRepository) Create(ctx context.Context, c *domainCustomer.Customer) error {
	client := r.client.Querier(ctx)

	r.log.Debug("creating customer",
		"customer_id", c.ID,
		"tenant_id", c.TenantID,
		"external_id", c.ExternalID,
	)

	customer, err := client.Customer.Create().
		SetID(c.ID).
		SetTenantID(c.TenantID).
		SetExternalID(c.ExternalID).
		SetName(c.Name).
		SetEmail(c.Email).
		SetStatus(string(c.Status)).
		SetCreatedAt(c.CreatedAt).
		SetUpdatedAt(c.UpdatedAt).
		SetCreatedBy(c.CreatedBy).
		SetUpdatedBy(c.UpdatedBy).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create customer: %w", err)
	}

	*c = *domainCustomer.FromEnt(customer)
	return nil
}

func (r *customerRepository) Get(ctx context.Context, id string) (*domainCustomer.Customer, error) {
	client := r.client.Querier(ctx)

	r.log.Debug("getting customer",
		"customer_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	c, err := client.Customer.Query().
		Where(
			customer.ID(id),
			customer.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("customer not found")
		}
		return nil, fmt.Errorf("failed to get customer: %w", err)
	}

	return domainCustomer.FromEnt(c), nil
}

func (r *customerRepository) List(ctx context.Context, filter types.Filter) ([]*domainCustomer.Customer, error) {
	client := r.client.Querier(ctx)

	r.log.Debug("listing customers",
		"tenant_id", types.GetTenantID(ctx),
		"limit", filter.Limit,
		"offset", filter.Offset,
	)

	customers, err := client.Customer.Query().
		Where(customer.TenantID(types.GetTenantID(ctx))).
		Order(ent.Desc(customer.FieldCreatedAt)).
		Limit(filter.Limit).
		Offset(filter.Offset).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to list customers: %w", err)
	}

	return domainCustomer.FromEntList(customers), nil
}

func (r *customerRepository) Update(ctx context.Context, c *domainCustomer.Customer) error {
	client := r.client.Querier(ctx)

	r.log.Debug("updating customer",
		"customer_id", c.ID,
		"tenant_id", c.TenantID,
	)

	_, err := client.Customer.Update().
		Where(
			customer.ID(c.ID),
			customer.TenantID(c.TenantID),
		).
		SetExternalID(c.ExternalID).
		SetName(c.Name).
		SetEmail(c.Email).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("customer not found")
		}
		return fmt.Errorf("failed to update customer: %w", err)
	}

	return nil
}

func (r *customerRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debug("deleting customer",
		"customer_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Customer.Update().
		Where(
			customer.ID(id),
			customer.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("customer not found")
		}
		return fmt.Errorf("failed to delete customer: %w", err)
	}

	return nil
}
