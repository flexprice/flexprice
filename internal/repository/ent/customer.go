package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/customer"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/internal/cache"
	domainCustomer "github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type customerRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CustomerQueryOptions
	cache     cache.Cache
}

func NewCustomerRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCustomer.Repository {
	return &customerRepository{
		client:    client,
		log:       log,
		queryOpts: CustomerQueryOptions{},
		cache:     cache,
	}
}

func (r *customerRepository) Create(ctx context.Context, c *domainCustomer.Customer) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("creating customer",
		"customer_id", c.ID,
		"tenant_id", c.TenantID,
		"external_id", c.ExternalID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "customer", "create", map[string]interface{}{
		"customer_id": c.ID,
		"external_id": c.ExternalID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if c.EnvironmentID == "" {
		c.EnvironmentID = types.GetEnvironmentID(ctx)
	}

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
		SetNillableParentCustomerID(c.ParentCustomerID).
		SetUpdatedBy(c.UpdatedBy).
		SetEnvironmentID(c.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {

			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Idx_tenant_environment_external_id_unique {
					return ierr.WithError(err).
						WithHint("A customer with this identifier already exists").
						WithReportableDetails(map[string]any{
							"external_id": c.ExternalID,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
			return ierr.WithError(err).
				WithHint("Failed to create customer").
				WithReportableDetails(map[string]any{
					"external_id": c.ExternalID,
					"email":       c.Email,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create customer").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*c = *domainCustomer.FromEnt(customer)
	return nil
}

func (r *customerRepository) Get(ctx context.Context, id string) (*domainCustomer.Customer, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "customer", "get", map[string]interface{}{
		"customer_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedCustomer := r.GetCache(ctx, id); cachedCustomer != nil {
		return cachedCustomer, nil
	}

	client := r.client.Reader(ctx)
	r.log.Debugw("getting customer", "customer_id", id)

	c, err := client.Customer.Query().
		Where(
			customer.ID(id),
			customer.TenantID(types.GetTenantID(ctx)),
			customer.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Customer with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"customer_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get customer").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	customer := domainCustomer.FromEnt(c)

	// Set cache
	r.SetCache(ctx, customer)
	return customer, nil
}

func (r *customerRepository) GetByLookupKey(ctx context.Context, lookupKey string) (*domainCustomer.Customer, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "customer", "get_by_lookup_key", map[string]interface{}{
		"lookup_key": lookupKey,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedCustomer := r.GetCache(ctx, lookupKey); cachedCustomer != nil {
		return cachedCustomer, nil
	}

	client := r.client.Reader(ctx)

	r.log.Debugw("getting customer by lookup key", "lookup_key", lookupKey)

	c, err := client.Customer.Query().
		Where(
			customer.ExternalID(lookupKey),
			customer.TenantID(types.GetTenantID(ctx)),
			customer.Status(string(types.StatusPublished)),
			customer.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Customer with lookup key %s was not found", lookupKey).
				WithReportableDetails(map[string]any{
					"lookup_key": lookupKey,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get customer by lookup key").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCustomer.FromEnt(c), nil
}

func (r *customerRepository) List(ctx context.Context, filter *types.CustomerFilter) ([]*domainCustomer.Customer, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "customer", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Customer.Query()
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list customers").
			Mark(ierr.ErrDatabase)
	}
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	customers, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list customers").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCustomer.FromEntList(customers), nil
}

func (r *customerRepository) Count(ctx context.Context, filter *types.CustomerFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "customer", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Customer.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count customers").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *customerRepository) ListAll(ctx context.Context, filter *types.CustomerFilter) ([]*domainCustomer.Customer, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "customer", "list_all", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Customer.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)

	var err error
	query, err = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to apply query options").
			Mark(ierr.ErrDatabase)
	}

	customers, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list customers").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainCustomer.FromEntList(customers), nil
}

func (r *customerRepository) Update(ctx context.Context, c *domainCustomer.Customer) error {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating customer",
		"customer_id", c.ID,
		"tenant_id", c.TenantID,
		"external_id", c.ExternalID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "customer", "update", map[string]interface{}{
		"customer_id": c.ID,
		"external_id": c.ExternalID,
	})
	defer FinishSpan(span)

	_, err := client.Customer.Update().
		Where(
			customer.ID(c.ID),
			customer.TenantID(c.TenantID),
			customer.EnvironmentID(types.GetEnvironmentID(ctx)),
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
		SetNillableParentCustomerID(c.ParentCustomerID).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Customer with ID %s was not found", c.ID).
				WithReportableDetails(map[string]any{
					"customer_id": c.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("A customer with this identifier already exists").
				WithReportableDetails(map[string]any{
					"external_id": c.ExternalID,
					"email":       c.Email,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to update customer").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, c)
	return nil
}

func (r *customerRepository) Delete(ctx context.Context, domainCustomer *domainCustomer.Customer) error {

	r.log.Debugw("deleting customer",
		"customer_id", domainCustomer.ID,
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "customer", "delete", map[string]interface{}{
		"customer_id": domainCustomer.ID,
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)
	_, err := client.Customer.Update().
		Where(
			customer.ID(domainCustomer.ID),
			customer.TenantID(types.GetTenantID(ctx)),
			customer.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Customer with ID %s was not found", domainCustomer.ID).
				WithReportableDetails(map[string]any{
					"customer_id": domainCustomer.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete customer").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, domainCustomer)
	return nil
}

// CustomerQuery type alias for better readability
type CustomerQuery = *ent.CustomerQuery

// CustomerQueryOptions implements BaseQueryOptions for customer queries
type CustomerQueryOptions struct{}

func (o CustomerQueryOptions) ApplyTenantFilter(ctx context.Context, query CustomerQuery) CustomerQuery {
	return query.Where(customer.TenantIDEQ(types.GetTenantID(ctx)))
}

func (o CustomerQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CustomerQuery) CustomerQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(customer.EnvironmentIDEQ(environmentID))
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
	case "status":
		return customer.FieldStatus
	default:
		//unknown field
		return ""
	}
}

func (o CustomerQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in customer query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o CustomerQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CustomerFilter, query CustomerQuery) (CustomerQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	if f.ExternalID != "" {
		query = query.Where(customer.ExternalID(f.ExternalID))
	}

	if len(f.ParentCustomerIDs) > 0 {
		query = query.Where(customer.ParentCustomerIDIn(f.ParentCustomerIDs...))
	}

	if f.Email != "" {
		query = query.Where(customer.Email(f.Email))
	}

	if len(f.CustomerIDs) > 0 {
		query = query.Where(customer.IDIn(f.CustomerIDs...))
	}

	if len(f.ExternalIDs) > 0 {
		query = query.Where(customer.ExternalIDIn(f.ExternalIDs...))
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[CustomerQuery, predicate.Customer](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Customer { return predicate.Customer(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[CustomerQuery, customer.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) customer.OrderOption { return customer.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (r *customerRepository) SetCache(ctx context.Context, customer *domainCustomer.Customer) {

	span := cache.StartCacheSpan(ctx, "customer", "set", map[string]interface{}{
		"customer_id": customer.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Set both ID and external ID based cache entries
	custIdKey := cache.GenerateKey(cache.PrefixCustomer, tenantID, environmentID, customer.ID)
	extIDKey := cache.GenerateKey(cache.PrefixCustomer, tenantID, environmentID, customer.ExternalID)

	r.cache.Set(ctx, custIdKey, customer, cache.ExpiryDefaultInMemory)
	r.cache.Set(ctx, extIDKey, customer, cache.ExpiryDefaultInMemory)

	r.log.Debugw("cache set", "id_key", custIdKey, "ext_key", extIDKey)
}

func (r *customerRepository) GetCache(ctx context.Context, key string) *domainCustomer.Customer {

	span := cache.StartCacheSpan(ctx, "customer", "get", map[string]interface{}{
		"customer_id": key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixCustomer, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if customer, ok := value.(*domainCustomer.Customer); ok {
			r.log.Debugw("cache hit", "key", cacheKey)
			return customer
		}
	}
	return nil
}

func (r *customerRepository) DeleteCache(ctx context.Context, customer *domainCustomer.Customer) {
	span := cache.StartCacheSpan(ctx, "customer", "delete", map[string]interface{}{
		"customer_id": customer.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Delete ID-based cache first
	custIdKey := cache.GenerateKey(cache.PrefixCustomer, tenantID, environmentID, customer.ID)
	extIDKey := cache.GenerateKey(cache.PrefixCustomer, tenantID, environmentID, customer.ExternalID)
	r.cache.Delete(ctx, custIdKey)
	r.cache.Delete(ctx, extIDKey)
	r.log.Debugw("cache deleted", "ext_key", extIDKey)
}
