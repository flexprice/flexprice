package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/customer"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type customerRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CustomerQueryOptions
}

func NewCustomerRepository(client postgres.IClient, log *logger.Logger) domainCustomer.Repository {
	return &customerRepository{
		client:    client,
		log:       log,
		queryOpts: CustomerQueryOptions{},
	}
}

func (r *customerRepository) Create(ctx context.Context, c *domainCustomer.Customer) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating customer",
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
		SetAddressLine1(c.AddressLine1).
		SetAddressLine2(c.AddressLine2).
		SetAddressCity(c.AddressCity).
		SetAddressState(c.AddressState).
		SetAddressPostalCode(c.AddressPostalCode).
		SetAddressCountry(c.AddressCountry).
		SetMetadata(c.Metadata).
		SetStatus(string(c.Status)).
		SetCreatedAt(c.CreatedAt).
		SetUpdatedAt(c.UpdatedAt).
		SetCreatedBy(c.CreatedBy).
		SetUpdatedBy(c.UpdatedBy).
		Save(ctx)

	if err != nil {
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to create customer")
	}

	*c = *domainCustomer.FromEnt(customer)
	return nil
}

func (r *customerRepository) Get(ctx context.Context, id string) (*domainCustomer.Customer, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting customer", "customer_id", id)

	c, err := client.Customer.Query().
		Where(
			customer.ID(id),
			customer.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.Wrap(err, errors.ErrCodeNotFound, "customer not found")
		}
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to get customer")
	}

	return domainCustomer.FromEnt(c), nil
}

func (r *customerRepository) GetByLookupKey(ctx context.Context, lookupKey string) (*domainCustomer.Customer, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting customer by lookup key", "lookup_key", lookupKey)

	c, err := client.Customer.Query().
		Where(
			customer.ExternalID(lookupKey),
			customer.TenantID(types.GetTenantID(ctx)),
			customer.Status(string(types.StatusPublished)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, errors.Wrap(err, errors.ErrCodeNotFound, "customer not found")
		}
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to get customer by lookup key")
	}

	return domainCustomer.FromEnt(c), nil
}

func (r *customerRepository) List(ctx context.Context, filter *types.CustomerFilter) ([]*domainCustomer.Customer, error) {
	client := r.client.Querier(ctx)

	query := client.Customer.Query()
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	customers, err := query.All(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to list customers")
	}

	return domainCustomer.FromEntList(customers), nil
}

func (r *customerRepository) Count(ctx context.Context, filter *types.CustomerFilter) (int, error) {
	client := r.client.Querier(ctx)
	query := client.Customer.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to count customers")
	}

	return count, nil
}

func (r *customerRepository) ListAll(ctx context.Context, filter *types.CustomerFilter) ([]*domainCustomer.Customer, error) {
	client := r.client.Querier(ctx)

	query := client.Customer.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	customers, err := query.All(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to list customers")
	}

	return domainCustomer.FromEntList(customers), nil
}

func (r *customerRepository) Update(ctx context.Context, c *domainCustomer.Customer) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating customer",
		"customer_id", c.ID,
		"tenant_id", c.TenantID,
		"external_id", c.ExternalID,
	)

	_, err := client.Customer.Update().
		Where(
			customer.ID(c.ID),
			customer.TenantID(c.TenantID),
		).
		SetExternalID(c.ExternalID).
		SetName(c.Name).
		SetEmail(c.Email).
		SetAddressLine1(c.AddressLine1).
		SetAddressLine2(c.AddressLine2).
		SetAddressCity(c.AddressCity).
		SetAddressState(c.AddressState).
		SetAddressPostalCode(c.AddressPostalCode).
		SetAddressCountry(c.AddressCountry).
		SetMetadata(c.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return errors.Wrap(err, errors.ErrCodeNotFound, "customer not found")
		}
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to update customer")
	}

	return nil
}

func (r *customerRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting customer",
		"customer_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Customer.Update().
		Where(
			customer.ID(id),
			customer.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return errors.Wrap(err, errors.ErrCodeNotFound, "customer not found")
		}
		return errors.Wrap(err, errors.ErrCodeInvalidOperation, "failed to delete customer")
	}

	return nil
}

// CustomerQuery type alias for better readability
type CustomerQuery = *ent.CustomerQuery

// CustomerQueryOptions implements BaseQueryOptions for customer queries
type CustomerQueryOptions struct{}

func (o CustomerQueryOptions) ApplyTenantFilter(ctx context.Context, query CustomerQuery) CustomerQuery {
	tenantID := types.GetTenantID(ctx)
	if tenantID != "" {
		query = query.Where(customer.TenantID(tenantID))
	}
	return query
}

func (o CustomerQueryOptions) ApplyStatusFilter(query CustomerQuery, status string) CustomerQuery {
	if status == "" {
		return query.Where(customer.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(customer.Status(status))
}

func (o CustomerQueryOptions) ApplySortFilter(query CustomerQuery, field string, order string) CustomerQuery {
	if field != "" {
		if order == types.OrderDesc {
			query = query.Order(ent.Desc(o.GetFieldName(field)))
		} else {
			query = query.Order(ent.Asc(o.GetFieldName(field)))
		}
	}
	return query
}

func (o CustomerQueryOptions) ApplyPaginationFilter(query CustomerQuery, limit int, offset int) CustomerQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o CustomerQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return customer.FieldCreatedAt
	case "updated_at":
		return customer.FieldUpdatedAt
	case "name":
		return customer.FieldName
	case "email":
		return customer.FieldEmail
	case "external_id":
		return customer.FieldExternalID
	default:
		return customer.FieldCreatedAt
	}
}

func (o CustomerQueryOptions) applyEntityQueryOptions(ctx context.Context, f *types.CustomerFilter, query CustomerQuery) CustomerQuery {
	if f == nil {
		return query
	}

	if f.ExternalID != "" {
		query = query.Where(customer.ExternalID(f.ExternalID))
	}

	if f.Email != "" {
		query = query.Where(customer.Email(f.Email))
	}

	if len(f.CustomerIDs) > 0 {
		query = query.Where(customer.IDIn(f.CustomerIDs...))
	}

	return query
}
