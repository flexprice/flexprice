package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/plan"
	"github.com/flexprice/flexprice/ent/predicate"
	"github.com/flexprice/flexprice/internal/cache"
	domainPlan "github.com/flexprice/flexprice/internal/domain/plan"
	"github.com/flexprice/flexprice/internal/dsl"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type planRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts PlanQueryOptions
	cache     cache.Cache
}

func NewPlanRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainPlan.Repository {
	return &planRepository{
		client:    client,
		log:       log,
		queryOpts: PlanQueryOptions{},
		cache:     cache,
	}
}

func (r *planRepository) Create(ctx context.Context, p *domainPlan.Plan) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("creating plan",
		"plan_id", p.ID,
		"tenant_id", p.TenantID,
		"name", p.Name,
		"lookup_key", p.LookupKey,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "plan", "create", map[string]interface{}{
		"plan_id": p.ID,
		"name":    p.Name,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if p.EnvironmentID == "" {
		p.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	plan, err := client.Plan.Create().
		SetID(p.ID).
		SetName(p.Name).
		SetDescription(p.Description).
		SetLookupKey(p.LookupKey).
		SetTenantID(p.TenantID).
		SetStatus(string(p.Status)).
		SetCreatedAt(p.CreatedAt).
		SetUpdatedAt(p.UpdatedAt).
		SetCreatedBy(p.CreatedBy).
		SetUpdatedBy(p.UpdatedBy).
		SetEnvironmentID(p.EnvironmentID).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("Plan with this name already exists").
				WithReportableDetails(map[string]any{
					"plan_id":   p.ID,
					"plan_name": p.Name,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to create plan").
			WithReportableDetails(map[string]any{
				"plan_id":   p.ID,
				"plan_name": p.Name,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	*p = *domainPlan.FromEnt(plan)
	return nil
}

func (r *planRepository) Get(ctx context.Context, id string) (*domainPlan.Plan, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "plan", "get", map[string]interface{}{
		"plan_id": id,
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedPlan := r.GetCache(ctx, id); cachedPlan != nil {
		return cachedPlan, nil
	}

	client := r.client.Querier(ctx)

	r.log.Debugw("getting plan", "plan_id", id)

	plan, err := client.Plan.Query().
		Where(
			plan.ID(id),
			plan.EnvironmentID(types.GetEnvironmentID(ctx)),
			plan.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Plan with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"plan_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get plan").
			WithReportableDetails(map[string]any{
				"plan_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	planData := domainPlan.FromEnt(plan)
	r.SetCache(ctx, planData)
	return planData, nil
}

func (r *planRepository) List(ctx context.Context, filter *types.PlanFilter) ([]*domainPlan.Plan, error) {
	client := r.client.Querier(ctx)
	r.log.Debugw("listing plans",
		"tenant_id", types.GetTenantID(ctx),
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset(),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "plan", "list", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Plan.Query()
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list plans").
			Mark(ierr.ErrDatabase)
	}
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	plans, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list plans").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return domainPlan.FromEntList(plans), nil
}

func (r *planRepository) ListAll(ctx context.Context, filter *types.PlanFilter) ([]*domainPlan.Plan, error) {
	if filter == nil {
		filter = types.NewNoLimitPlanFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	plans, err := r.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	return plans, nil
}

func (r *planRepository) Count(ctx context.Context, filter *types.PlanFilter) (int, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("counting plans",
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "plan", "count", map[string]interface{}{
		"filter": filter,
	})
	defer FinishSpan(span)

	query := client.Plan.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query, err := r.queryOpts.applyEntityQueryOptions(ctx, filter, query)
	if err != nil {
		return 0, err
	}

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count plans").
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	return count, nil
}

func (r *planRepository) GetByLookupKey(ctx context.Context, lookupKey string) (*domainPlan.Plan, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "plan", "get_by_lookup_key", map[string]interface{}{
		"lookup_key": lookupKey,
	})
	defer FinishSpan(span)
	r.log.Debugw("getting plan by lookup key", "lookup_key", lookupKey)

	// Try to get from cache first
	if cachedPlan := r.GetCache(ctx, lookupKey); cachedPlan != nil {
		return cachedPlan, nil
	}

	plan, err := r.client.Querier(ctx).Plan.Query().
		Where(
			plan.LookupKey(lookupKey),
			plan.EnvironmentID(types.GetEnvironmentID(ctx)),
			plan.TenantID(types.GetTenantID(ctx)),
			plan.Status(string(types.StatusPublished)),
		).
		Only(ctx)
	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHintf("Plan with lookup key %s was not found", lookupKey).
				WithReportableDetails(map[string]any{
					"lookup_key": lookupKey,
				}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get plan by lookup key").
			WithReportableDetails(map[string]any{
				"lookup_key": lookupKey,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	planData := domainPlan.FromEnt(plan)
	r.SetCache(ctx, planData)
	return planData, nil
}

func (r *planRepository) Update(ctx context.Context, p *domainPlan.Plan) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating plan",
		"plan_id", p.ID,
		"tenant_id", p.TenantID,
		"name", p.Name,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "plan", "update", map[string]interface{}{
		"plan_id": p.ID,
		"name":    p.Name,
	})
	defer FinishSpan(span)

	_, err := client.Plan.Update().
		Where(
			plan.ID(p.ID),
			plan.TenantID(p.TenantID),
		).
		SetLookupKey(p.LookupKey).
		SetName(p.Name).
		SetDescription(p.Description).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Plan with ID %s was not found", p.ID).
				WithReportableDetails(map[string]any{
					"plan_id": p.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		if ent.IsConstraintError(err) {
			return ierr.WithError(err).
				WithHint("Plan with this name already exists").
				WithReportableDetails(map[string]any{
					"plan_id":   p.ID,
					"plan_name": p.Name,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return ierr.WithError(err).
			WithHint("Failed to update plan").
			WithReportableDetails(map[string]any{
				"plan_id": p.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, p)
	return nil
}

func (r *planRepository) Delete(ctx context.Context, p *domainPlan.Plan) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting plan",
		"plan_id", p.ID,
		"tenant_id", types.GetTenantID(ctx),
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "plan", "delete", map[string]interface{}{
		"plan_id": p.ID,
	})
	defer FinishSpan(span)

	_, err := client.Plan.Update().
		Where(
			plan.ID(p.ID),
			plan.EnvironmentID(types.GetEnvironmentID(ctx)),
			plan.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)

		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Plan with ID %s was not found", p.ID).
				WithReportableDetails(map[string]any{
					"plan_id": p.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete plan").
			WithReportableDetails(map[string]any{
				"plan_id": p.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	SetSpanSuccess(span)
	r.DeleteCache(ctx, p)
	return nil
}

// PlanQuery type alias for better readability
type PlanQuery = *ent.PlanQuery

// PlanQueryOptions implements BaseQueryOptions for plan queries
type PlanQueryOptions struct{}

func (o PlanQueryOptions) ApplyTenantFilter(ctx context.Context, query PlanQuery) PlanQuery {
	return query.Where(plan.TenantID(types.GetTenantID(ctx)))
}

func (o PlanQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query PlanQuery) PlanQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(plan.EnvironmentID(environmentID))
	}
	return query
}

func (o PlanQueryOptions) ApplyStatusFilter(query PlanQuery, status string) PlanQuery {
	if status == "" {
		return query.Where(plan.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(plan.Status(status))
}

func (o PlanQueryOptions) ApplySortFilter(query PlanQuery, field string, order string) PlanQuery {
	field = o.GetFieldName(field)
	if order == types.OrderDesc {
		return query.Order(ent.Desc(field))
	}
	return query.Order(ent.Asc(field))
}

func (o PlanQueryOptions) ApplyPaginationFilter(query PlanQuery, limit int, offset int) PlanQuery {
	return query.Offset(offset).Limit(limit)
}

func (o PlanQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return plan.FieldCreatedAt
	case "updated_at":
		return plan.FieldUpdatedAt
	case "lookup_key":
		return plan.FieldLookupKey
	case "name":
		return plan.FieldName
	default:
		return field
	}
}

func (o PlanQueryOptions) GetFieldResolver(field string) (string, error) {
	fieldName := o.GetFieldName(field)
	if fieldName == "" {
		return "", ierr.NewErrorf("unknown field name '%s' in plan query", field).
			Mark(ierr.ErrValidation)
	}
	return fieldName, nil
}

func (o PlanQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.PlanFilter, query PlanQuery) (PlanQuery, error) {
	var err error
	if f == nil {
		return query, nil
	}

	if len(f.PlanIDs) > 0 {
		query = query.Where(plan.IDIn(f.PlanIDs...))
	}

	if f.Filters != nil {
		query, err = dsl.ApplyFilters[PlanQuery, predicate.Plan](
			query,
			f.Filters,
			o.GetFieldResolver,
			func(p dsl.Predicate) predicate.Plan { return predicate.Plan(p) },
		)
		if err != nil {
			return nil, err
		}
	}

	// Apply sorts using the generic function
	if f.Sort != nil {
		query, err = dsl.ApplySorts[PlanQuery, plan.OrderOption](
			query,
			f.Sort,
			o.GetFieldResolver,
			func(o dsl.OrderFunc) plan.OrderOption { return plan.OrderOption(o) },
		)
		if err != nil {
			return nil, err
		}
	}

	return query, nil
}

func (r *planRepository) SetCache(ctx context.Context, plan *domainPlan.Plan) {

	span := cache.StartCacheSpan(ctx, "plan", "set", map[string]interface{}{
		"plan_id": plan.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPlan, tenantID, environmentID, plan.ID)
	r.cache.Set(ctx, cacheKey, plan, cache.ExpiryDefaultInMemory)
}

func (r *planRepository) GetCache(ctx context.Context, key string) *domainPlan.Plan {

	span := cache.StartCacheSpan(ctx, "plan", "get", map[string]interface{}{
		"plan_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPlan, tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainPlan.Plan)
	}
	return nil
}

func (r *planRepository) DeleteCache(ctx context.Context, plan *domainPlan.Plan) {
	span := cache.StartCacheSpan(ctx, "plan", "delete", map[string]interface{}{
		"plan_id": plan.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	cacheKey := cache.GenerateKey(cache.PrefixPlan, tenantID, environmentID, plan.ID)
	lookupCacheKey := cache.GenerateKey(cache.PrefixPlan, tenantID, environmentID, plan.LookupKey)
	r.cache.Delete(ctx, cacheKey)
	r.cache.Delete(ctx, lookupCacheKey)
}
