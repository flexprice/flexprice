package ent

import (
	"context"
	"errors"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/defaulttaxrateconfig"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/ent/schema"
	"github.com/flexprice/flexprice/ent/taxrate"
	"github.com/flexprice/flexprice/internal/cache"
	domainTaxRate "github.com/flexprice/flexprice/internal/domain/tax"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/lib/pq"
)

type taxrateRepository struct {
	client                        postgres.IClient
	log                           *logger.Logger
	queryOpts                     TaxRateQueryOptions
	defaultTaxRateConfigQueryOpts DefaultTaxRateConfigQueryOptions
	cache                         cache.Cache
}

func NewTaxRateRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainTaxRate.Repository {
	return &taxrateRepository{
		client:                        client,
		log:                           log,
		queryOpts:                     TaxRateQueryOptions{},
		defaultTaxRateConfigQueryOpts: DefaultTaxRateConfigQueryOptions{},
		cache:                         cache,
	}
}

func (r *taxrateRepository) Create(ctx context.Context, t *domainTaxRate.TaxRate) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxrate", "create", map[string]interface{}{
		"taxrate_id": t.ID,
		"name":       t.Name,
		"code":       t.Code,
		"tenant_id":  t.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("creating taxrate",
		"taxrate_id", t.ID,
		"taxrate_name", t.Name,
		"taxrate_code", t.Code,
		"tenant_id", t.TenantID,
	)

	// set env id if not set
	if t.EnvironmentID == "" {
		t.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	_, err := client.TaxRate.Create().
		SetID(t.ID).
		SetName(t.Name).
		SetCode(t.Code).
		SetDescription(t.Description).
		SetMetadata(t.Metadata).
		SetTaxRateType(string(t.TaxRateType)).
		SetNillablePercentageValue(t.PercentageValue).
		SetNillableFixedValue(t.FixedValue).
		SetScope(string(t.Scope)).
		SetNillableValidFrom(t.ValidFrom).
		SetNillableValidTo(t.ValidTo).
		SetStatus(string(t.Status)).
		SetTenantID(t.TenantID).
		SetTaxRateStatus(string(t.TaxRateStatus)).
		SetTaxRateType(string(t.TaxRateType)).
		SetEnvironmentID(t.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("error creating taxrate", "error", err)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Idx_code_tenant_id_environment_id {
					return ierr.WithError(err).
						WithHint("A taxrate with this code already exists").
						WithReportableDetails(map[string]any{
							"taxrate_code": t.Code,
							"taxrate_id":   t.ID,
							"taxrate_name": t.Name,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
		}
		return ierr.WithError(err).
			WithHint("Failed to create taxrate").
			WithReportableDetails(map[string]any{
				"taxrate_id":   t.ID,
				"taxrate_name": t.Name,
				"taxrate_code": t.Code,
			}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *taxrateRepository) Get(ctx context.Context, id string) (*domainTaxRate.TaxRate, error) {
	// set span
	span := StartRepositorySpan(ctx, "taxrate", "get", map[string]interface{}{
		"taxrate_id": id,
	})
	defer FinishSpan(span)

	// get from cache if exists
	if taxrate := r.GetCache(ctx, id); taxrate != nil {
		return taxrate, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting taxrate",
		"id", id,
	)

	taxrateEnt, err := client.TaxRate.Query().
		Where(
			taxrate.ID(id),
			taxrate.TenantID(types.GetTenantID(ctx)),
			taxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Taxrate with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"taxrate_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get taxrate").
			Mark(ierr.ErrDatabase)
	}

	taxrate := domainTaxRate.FromEnt(taxrateEnt)
	r.SetCache(ctx, taxrate)

	return taxrate, nil
}

func (r *taxrateRepository) List(ctx context.Context, filter *types.TaxRateFilter) ([]*domainTaxRate.TaxRate, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxrate", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Invalid filter").
			Mark(ierr.ErrValidation)
	}

	query := r.client.Querier(ctx).TaxRate.Query()

	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list taxrates").
			Mark(ierr.ErrDatabase)
	}

	taxrates, err := query.
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list taxrates").
			Mark(ierr.ErrDatabase)
	}

	r.log.Debugw("listing taxrates", "filter", filter)
	SetSpanSuccess(span)
	return domainTaxRate.FromEntList(taxrates), nil
}

func (r *taxrateRepository) Count(ctx context.Context, filter *types.TaxRateFilter) (int, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxrate", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := r.client.Querier(ctx).TaxRate.Query()

	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count taxrates").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count taxrates").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *taxrateRepository) Update(ctx context.Context, t *domainTaxRate.TaxRate) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxrate", "update", map[string]interface{}{
		"taxrate_id": t.ID,
	})
	defer FinishSpan(span)

	r.log.Debugw("updating taxrate", "taxrate", t)

	client := r.client.Querier(ctx)

	_, err := client.TaxRate.Update().
		Where(
			taxrate.ID(t.ID),
			taxrate.TenantID(types.GetTenantID(ctx)),
			taxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(t.Name).
		SetCode(t.Code).
		SetDescription(t.Description).
		SetMetadata(t.Metadata).
		SetNillablePercentageValue(t.PercentageValue).
		SetNillableFixedValue(t.FixedValue).
		SetScope(string(t.Scope)).
		SetNillableValidFrom(t.ValidFrom).
		SetNillableValidTo(t.ValidTo).
		SetStatus(string(t.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("error updating taxrate", "error", err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Taxrate with ID %s was not found", t.ID).
				WithReportableDetails(map[string]any{
					"taxrate_id": t.ID,
				}).
				Mark(ierr.ErrNotFound)
		}

		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Idx_code_tenant_id_environment_id {
					return ierr.WithError(err).
						WithHint("A taxrate with this code already exists").
						WithReportableDetails(map[string]any{
							"taxrate_code": t.Code,
							"taxrate_id":   t.ID,
							"taxrate_name": t.Name,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
		}

		return ierr.WithError(err).
			WithHint("Failed to update taxrate").
			WithReportableDetails(map[string]any{
				"taxrate_id": t.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, t)
	return nil
}

func (r *taxrateRepository) Delete(ctx context.Context, t *domainTaxRate.TaxRate) error {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxrate", "delete", map[string]interface{}{
		"taxrate_id": t.ID,
	})
	defer FinishSpan(span)

	r.log.Debugw("deleting taxrate",
		"taxrate_id", t.ID,
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	)
	client := r.client.Querier(ctx)
	_, err := client.TaxRate.Update().
		Where(
			taxrate.ID(t.ID),
			taxrate.TenantID(types.GetTenantID(ctx)),
			taxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete taxrate").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, t)
	return nil
}

func (r *taxrateRepository) GetByCode(ctx context.Context, code string) (*domainTaxRate.TaxRate, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxrate", "get_by_code", map[string]interface{}{
		"taxrate_code": code,
	})
	defer FinishSpan(span)

	r.log.Debugw("getting taxrate by code", "code", code)
	client := r.client.Querier(ctx)
	taxrateEnt, err := client.TaxRate.Query().
		Where(
			taxrate.Code(code),
			taxrate.TenantID(types.GetTenantID(ctx)),
			taxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Taxrate with code %s was not found", code).
				WithReportableDetails(map[string]any{
					"taxrate_code": code,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get taxrate by code").
			Mark(ierr.ErrDatabase)
	}

	taxrate := domainTaxRate.FromEnt(taxrateEnt)
	r.SetCache(ctx, taxrate)
	return taxrate, nil
}

func (r *taxrateRepository) ListAll(ctx context.Context, filter *types.TaxRateFilter) ([]*domainTaxRate.TaxRate, error) {

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "taxrate", "list_all", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Invalid filter").
			Mark(ierr.ErrValidation)
	}

	client := r.client.Querier(ctx)
	taxrates, err := client.TaxRate.Query().
		Where(
			taxrate.TenantID(types.GetTenantID(ctx)),
			taxrate.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		All(ctx)

	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list all taxrates").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainTaxRate.FromEntList(taxrates), nil
}

// TaxRateQuery type alias for better readability
type TaxRateQuery = *ent.TaxRateQuery

// TaxRateQueryOptions implements BaseQueryOptions for taxrate queries
type TaxRateQueryOptions struct{}

func (o TaxRateQueryOptions) ApplyTenantFilter(ctx context.Context, query TaxRateQuery) TaxRateQuery {
	return query.Where(taxrate.TenantID(types.GetTenantID(ctx)))
}

func (o TaxRateQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query TaxRateQuery) TaxRateQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(taxrate.EnvironmentID(environmentID))
	}
	return query
}

func (o TaxRateQueryOptions) ApplyStatusFilter(query TaxRateQuery, status string) TaxRateQuery {
	if status == "" {
		return query.Where(taxrate.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(taxrate.Status(status))
}

func (o TaxRateQueryOptions) ApplySortFilter(query TaxRateQuery, field string, order string) TaxRateQuery {
	field = o.GetFieldName(field)
	if order == types.OrderDesc {
		return query.Order(ent.Desc(field))
	}
	return query.Order(ent.Asc(field))
}

func (o TaxRateQueryOptions) ApplyPaginationFilter(query TaxRateQuery, limit int, offset int) TaxRateQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName returns the field name for the given field
// @Returns: the field name for the given field
func (o TaxRateQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return taxrate.FieldCreatedAt
	case "updated_at":
		return taxrate.FieldUpdatedAt
	case "name":
		return taxrate.FieldName
	case "code":
		return taxrate.FieldCode
	case "percentage_value":
		return taxrate.FieldPercentageValue
	case "fixed_value":
		return taxrate.FieldFixedValue
	case "scope":
		return taxrate.FieldScope
	case "valid_from":
		return taxrate.FieldValidFrom
	case "valid_to":
		return taxrate.FieldValidTo
	case "tax_rate_type":
		return taxrate.FieldTaxRateType
	case "status":
		return taxrate.FieldStatus
	default:
		return ""
	}
}

func (o TaxRateQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in taxrate query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o TaxRateQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.TaxRateFilter, query TaxRateQuery) (TaxRateQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	if f.Code != "" {
		query = query.Where(taxrate.Code(f.Code))
	}

	if f.TaxRateIDs != nil {
		query = query.Where(taxrate.IDIn(f.TaxRateIDs...))
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[TaxRateQuery, predicate.TaxRate](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.TaxRate { return predicate.TaxRate(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[TaxRateQuery, taxrate.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) taxrate.OrderOption { return taxrate.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}
	return query, nil
}

// caching
func (r *taxrateRepository) SetCache(ctx context.Context, taxrate *domainTaxRate.TaxRate) {

	span := cache.StartCacheSpan(ctx, "taxrate", "set", map[string]interface{}{
		"taxrate_id": taxrate.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Set both ID and external ID based cache entries
	cacheKey := cache.GenerateKey(cache.PrefixTaxRate, tenantID, environmentID, taxrate.ID)
	r.cache.Set(ctx, cacheKey, taxrate, cache.ExpiryDefaultInMemory)

	r.log.Debugw("cache set", "id_key", cacheKey)
}

func (r *taxrateRepository) GetCache(ctx context.Context, key string) *domainTaxRate.TaxRate {

	span := cache.StartCacheSpan(ctx, "taxrate", "get", map[string]interface{}{
		"taxrate_id": key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixTaxRate, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if taxrate, ok := value.(*domainTaxRate.TaxRate); ok {
			r.log.Debugw("cache hit", "key", cacheKey)
			return taxrate
		}
	}
	r.log.Debugw("cache miss", "key", cacheKey)
	return nil
}

func (r *taxrateRepository) DeleteCache(ctx context.Context, taxrate *domainTaxRate.TaxRate) {
	span := cache.StartCacheSpan(ctx, "taxrate", "delete", map[string]interface{}{
		"taxrate_id": taxrate.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	// Delete ID-based cache first
	cacheKey := cache.GenerateKey(cache.PrefixTaxRate, tenantID, environmentID, taxrate.ID)
	r.cache.Delete(ctx, cacheKey)
	r.log.Debugw("cache deleted", "key", cacheKey)
}

// DefaultTaxRateConfig operations
func (r *taxrateRepository) GetDefaultTaxRateConfigByID(ctx context.Context, id string) (*domainTaxRate.DefaultTaxRateConfig, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "defaulttaxrateconfig", "get_by_id", map[string]interface{}{
		"config_id": id,
	})
	defer FinishSpan(span)

	// get from cache if exists
	if config := r.GetDefaultTaxRateConfigCache(ctx, id); config != nil {
		return config, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting default tax rate config by id", "id", id)

	configEnt, err := client.DefaultTaxRateConfig.Query().
		Where(
			defaulttaxrateconfig.ID(id),
			defaulttaxrateconfig.TenantID(types.GetTenantID(ctx)),
			defaulttaxrateconfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Default tax rate config with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"config_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get default tax rate config").
			Mark(ierr.ErrDatabase)
	}

	config := &domainTaxRate.DefaultTaxRateConfig{}
	result := config.FromEnt(configEnt)
	r.SetDefaultTaxRateConfigCache(ctx, result)

	SetSpanSuccess(span)
	return result, nil
}

func (r *taxrateRepository) ListDefaultTaxRateConfigs(ctx context.Context, filter *types.DefaultTaxRateConfigFilter) ([]*domainTaxRate.DefaultTaxRateConfig, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "defaulttaxrateconfig", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Invalid filter").
			Mark(ierr.ErrValidation)
	}

	query := r.client.Querier(ctx).DefaultTaxRateConfig.Query()

	query = ApplyQueryOptions(ctx, query, filter, r.defaultTaxRateConfigQueryOpts)

	query, err := r.defaultTaxRateConfigQueryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list default tax rate configs").
			Mark(ierr.ErrDatabase)
	}

	configs, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list default tax rate configs").
			Mark(ierr.ErrDatabase)
	}

	r.log.Debugw("listing default tax rate configs", "filter", filter)
	SetSpanSuccess(span)
	return fromDefaultTaxRateConfigEntList(configs), nil
}

func (r *taxrateRepository) CountDefaultTaxRateConfigs(ctx context.Context, filter *types.DefaultTaxRateConfigFilter) (int, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "defaulttaxrateconfig", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	if err := filter.Validate(); err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Invalid filter").
			Mark(ierr.ErrValidation)
	}

	query := r.client.Querier(ctx).DefaultTaxRateConfig.Query()

	query, err := r.defaultTaxRateConfigQueryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count default tax rate configs").
			Mark(ierr.ErrDatabase)
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count default tax rate configs").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *taxrateRepository) CreateDefaultTaxRateConfig(ctx context.Context, config *domainTaxRate.DefaultTaxRateConfig) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "defaulttaxrateconfig", "create", map[string]interface{}{
		"config_id":   config.ID,
		"tax_rate_id": config.TaxRateID,
		"entity_type": config.EntityType,
		"entity_id":   config.EntityID,
		"tenant_id":   config.TenantID,
	})
	defer FinishSpan(span)

	client := r.client.Querier(ctx)

	r.log.Debugw("creating default tax rate config",
		"config_id", config.ID,
		"tax_rate_id", config.TaxRateID,
		"entity_type", config.EntityType,
		"entity_id", config.EntityID,
		"tenant_id", config.TenantID,
	)

	// set env id if not set
	if config.EnvironmentID == "" {
		config.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	createBuilder := client.DefaultTaxRateConfig.Create().
		SetID(config.ID).
		SetTaxRateID(config.TaxRateID).
		SetEntityType(config.EntityType).
		SetEntityID(config.EntityID).
		SetPriority(config.Priority).
		SetAutoApply(config.AutoApply).
		SetNillableValidFrom(config.ValidFrom).
		SetNillableValidTo(config.ValidTo).
		SetMetadata(config.Metadata).
		SetTenantID(config.TenantID).
		SetEnvironmentID(config.EnvironmentID).
		SetCreatedBy(types.GetUserID(ctx)).
		SetUpdatedBy(types.GetUserID(ctx))

	// Handle currency field if not empty
	if config.Currency != "" {
		createBuilder = createBuilder.SetCurrency(config.Currency)
	}

	_, err := createBuilder.Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("error creating default tax rate config", "error", err)
		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Unique_entity_tax_mapping {
					return ierr.WithError(err).
						WithHint("A default tax rate config with this entity and tax rate already exists").
						WithReportableDetails(map[string]any{
							"config_id":   config.ID,
							"tax_rate_id": config.TaxRateID,
							"entity_type": config.EntityType,
							"entity_id":   config.EntityID,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
		}
		return ierr.WithError(err).
			WithHint("Failed to create default tax rate config").
			WithReportableDetails(map[string]any{
				"config_id":   config.ID,
				"tax_rate_id": config.TaxRateID,
				"entity_type": config.EntityType,
				"entity_id":   config.EntityID,
			}).
			Mark(ierr.ErrDatabase)
	}
	SetSpanSuccess(span)
	return nil
}

func (r *taxrateRepository) UpdateDefaultTaxRateConfig(ctx context.Context, id string, config *domainTaxRate.DefaultTaxRateConfig) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "defaulttaxrateconfig", "update", map[string]interface{}{
		"config_id": id,
	})
	defer FinishSpan(span)

	r.log.Debugw("updating default tax rate config", "config_id", id, "config", config)

	client := r.client.Querier(ctx)

	updateBuilder := client.DefaultTaxRateConfig.Update().
		Where(
			defaulttaxrateconfig.ID(id),
			defaulttaxrateconfig.TenantID(types.GetTenantID(ctx)),
			defaulttaxrateconfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetEntityID(config.EntityID).
		SetPriority(config.Priority).
		SetAutoApply(config.AutoApply).
		SetNillableValidFrom(config.ValidFrom).
		SetNillableValidTo(config.ValidTo).
		SetMetadata(config.Metadata).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx))

	_, err := updateBuilder.Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		r.log.Errorw("error updating default tax rate config", "error", err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Default tax rate config with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"config_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}

		if ent.IsConstraintError(err) {
			var pqErr *pq.Error
			if errors.As(err, &pqErr) {
				if pqErr.Constraint == schema.Unique_entity_tax_mapping {
					return ierr.WithError(err).
						WithHint("A default tax rate config with this entity and tax rate already exists").
						WithReportableDetails(map[string]any{
							"config_id":   id,
							"tax_rate_id": config.TaxRateID,
							"entity_type": config.EntityType,
							"entity_id":   config.EntityID,
						}).
						Mark(ierr.ErrAlreadyExists)
				}
			}
		}

		return ierr.WithError(err).
			WithHint("Failed to update default tax rate config").
			WithReportableDetails(map[string]any{
				"config_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteDefaultTaxRateConfigCache(ctx, &domainTaxRate.DefaultTaxRateConfig{ID: id})
	return nil
}

func (r *taxrateRepository) DeleteDefaultTaxRateConfig(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "defaulttaxrateconfig", "delete", map[string]interface{}{
		"config_id": id,
	})
	defer FinishSpan(span)

	r.log.Debugw("deleting default tax rate config",
		"config_id", id,
		"tenant_id", types.GetTenantID(ctx),
		"environment_id", types.GetEnvironmentID(ctx),
	)

	client := r.client.Querier(ctx)
	_, err := client.DefaultTaxRateConfig.Update().
		Where(
			defaulttaxrateconfig.ID(id),
			defaulttaxrateconfig.TenantID(types.GetTenantID(ctx)),
			defaulttaxrateconfig.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Default tax rate config with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"config_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete default tax rate config").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteDefaultTaxRateConfigCache(ctx, &domainTaxRate.DefaultTaxRateConfig{ID: id})
	return nil
}

// DefaultTaxRateConfigQuery type alias for better readability
type DefaultTaxRateConfigQuery = *ent.DefaultTaxRateConfigQuery

// DefaultTaxRateConfigQueryOptions implements BaseQueryOptions for default tax rate config queries
type DefaultTaxRateConfigQueryOptions struct{}

func (o DefaultTaxRateConfigQueryOptions) ApplyTenantFilter(ctx context.Context, query DefaultTaxRateConfigQuery) DefaultTaxRateConfigQuery {
	return query.Where(defaulttaxrateconfig.TenantID(types.GetTenantID(ctx)))
}

func (o DefaultTaxRateConfigQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query DefaultTaxRateConfigQuery) DefaultTaxRateConfigQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(defaulttaxrateconfig.EnvironmentID(environmentID))
	}
	return query
}

func (o DefaultTaxRateConfigQueryOptions) ApplyStatusFilter(query DefaultTaxRateConfigQuery, status string) DefaultTaxRateConfigQuery {
	if status == "" {
		return query.Where(defaulttaxrateconfig.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(defaulttaxrateconfig.Status(status))
}

func (o DefaultTaxRateConfigQueryOptions) ApplySortFilter(query DefaultTaxRateConfigQuery, field string, order string) DefaultTaxRateConfigQuery {
	field = o.GetFieldName(field)
	if order == types.OrderDesc {
		return query.Order(ent.Desc(field))
	}
	return query.Order(ent.Asc(field))
}

func (o DefaultTaxRateConfigQueryOptions) ApplyPaginationFilter(query DefaultTaxRateConfigQuery, limit int, offset int) DefaultTaxRateConfigQuery {
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

// GetFieldName returns the field name for the given field
func (o DefaultTaxRateConfigQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return defaulttaxrateconfig.FieldCreatedAt
	case "updated_at":
		return defaulttaxrateconfig.FieldUpdatedAt
	case "tax_rate_id":
		return defaulttaxrateconfig.FieldTaxRateID
	case "entity_type":
		return defaulttaxrateconfig.FieldEntityType
	case "entity_id":
		return defaulttaxrateconfig.FieldEntityID
	case "priority":
		return defaulttaxrateconfig.FieldPriority
	case "auto_apply":
		return defaulttaxrateconfig.FieldAutoApply
	case "valid_from":
		return defaulttaxrateconfig.FieldValidFrom
	case "valid_to":
		return defaulttaxrateconfig.FieldValidTo
	case "currency":
		return defaulttaxrateconfig.FieldCurrency
	case "status":
		return defaulttaxrateconfig.FieldStatus
	default:
		return ""
	}
}

func (o DefaultTaxRateConfigQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in default tax rate config query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o DefaultTaxRateConfigQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.DefaultTaxRateConfigFilter, query DefaultTaxRateConfigQuery) (DefaultTaxRateConfigQuery, error) {
	if f == nil {
		return query, nil
	}

	if f.DefaultTaxRateConfigIDs != nil {
		query = query.Where(defaulttaxrateconfig.IDIn(f.DefaultTaxRateConfigIDs...))
	}

	if f.TaxRateIDs != nil {
		query = query.Where(defaulttaxrateconfig.TaxRateIDIn(f.TaxRateIDs...))
	}

	if f.EntityType != "" {
		query = query.Where(defaulttaxrateconfig.EntityType(f.EntityType))
	}

	if f.EntityID != "" {
		query = query.Where(defaulttaxrateconfig.EntityID(f.EntityID))
	}

	if f.Currency != "" {
		query = query.Where(defaulttaxrateconfig.Currency(f.Currency))
	}

	return query, nil
}

// DefaultTaxRateConfig caching methods
func (r *taxrateRepository) SetDefaultTaxRateConfigCache(ctx context.Context, config *domainTaxRate.DefaultTaxRateConfig) {
	span := cache.StartCacheSpan(ctx, "defaulttaxrateconfig", "set", map[string]interface{}{
		"config_id": config.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cacheKey := cache.GenerateKey(cache.PrefixDefaultTaxRateConfig, tenantID, environmentID, config.ID)
	r.cache.Set(ctx, cacheKey, config, cache.ExpiryDefaultInMemory)

	r.log.Debugw("default tax rate config cache set", "id_key", cacheKey)
}

func (r *taxrateRepository) GetDefaultTaxRateConfigCache(ctx context.Context, key string) *domainTaxRate.DefaultTaxRateConfig {
	span := cache.StartCacheSpan(ctx, "defaulttaxrateconfig", "get", map[string]interface{}{
		"config_id": key,
	})
	defer cache.FinishSpan(span)

	cacheKey := cache.GenerateKey(cache.PrefixDefaultTaxRateConfig, types.GetTenantID(ctx), types.GetEnvironmentID(ctx), key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		if config, ok := value.(*domainTaxRate.DefaultTaxRateConfig); ok {
			r.log.Debugw("default tax rate config cache hit", "key", cacheKey)
			return config
		}
	}
	r.log.Debugw("default tax rate config cache miss", "key", cacheKey)
	return nil
}

func (r *taxrateRepository) DeleteDefaultTaxRateConfigCache(ctx context.Context, config *domainTaxRate.DefaultTaxRateConfig) {
	span := cache.StartCacheSpan(ctx, "defaulttaxrateconfig", "delete", map[string]interface{}{
		"config_id": config.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)

	cacheKey := cache.GenerateKey(cache.PrefixDefaultTaxRateConfig, tenantID, environmentID, config.ID)
	r.cache.Delete(ctx, cacheKey)
	r.log.Debugw("default tax rate config cache deleted", "key", cacheKey)
}

// Helper function to convert ent list to domain list
func fromDefaultTaxRateConfigEntList(ents []*ent.DefaultTaxRateConfig) []*domainTaxRate.DefaultTaxRateConfig {
	var configs []*domainTaxRate.DefaultTaxRateConfig
	for _, ent := range ents {
		config := &domainTaxRate.DefaultTaxRateConfig{}
		configs = append(configs, config.FromEnt(ent))
	}
	return configs
}
