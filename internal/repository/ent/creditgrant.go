package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/creditgrant"
	"github.com/flexprice/flexprice/internal/cache"
	domainCreditGrant "github.com/flexprice/flexprice/internal/domain/creditgrant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type creditGrantRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts CreditGrantQueryOptions
	cache     cache.Cache
}

func NewCreditGrantRepository(client postgres.IClient, log *logger.Logger, cache cache.Cache) domainCreditGrant.Repository {
	return &creditGrantRepository{
		client:    client,
		log:       log,
		queryOpts: CreditGrantQueryOptions{},
		cache:     cache,
	}
}

func (r *creditGrantRepository) Create(ctx context.Context, cg *domainCreditGrant.CreditGrant) (*domainCreditGrant.CreditGrant, error) {
	if err := cg.Validate(); err != nil {
		return nil, err
	}

	client := r.client.Writer(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "create", map[string]interface{}{
		"scope":     cg.Scope,
		"plan_id":   cg.PlanID,
		"tenant_id": cg.TenantID,
	})
	defer FinishSpan(span)

	// Set environment ID from context if not already set
	if cg.EnvironmentID == "" {
		cg.EnvironmentID = types.GetEnvironmentID(ctx)
	}

	create := client.CreditGrant.Create().
		SetID(cg.ID).
		SetName(cg.Name).
		SetScope(cg.Scope).
		SetCredits(cg.Credits).
		SetCadence(cg.Cadence).
		SetNillablePlanID(cg.PlanID).
		SetNillableSubscriptionID(cg.SubscriptionID).
		SetNillablePeriod(cg.Period).
		SetNillablePeriodCount(cg.PeriodCount).
		SetExpirationType(cg.ExpirationType).
		SetNillableExpirationDuration(cg.ExpirationDuration).
		SetNillableExpirationDurationUnit(cg.ExpirationDurationUnit).
		SetNillablePriority(cg.Priority).
		SetNillableStartDate(cg.StartDate).
		SetNillableEndDate(cg.EndDate).
		SetNillableCreditGrantAnchor(cg.CreditGrantAnchor).
		SetTenantID(cg.TenantID).
		SetStatus(string(cg.Status)).
		SetCreatedAt(cg.CreatedAt).
		SetUpdatedAt(cg.UpdatedAt).
		SetCreatedBy(cg.CreatedBy).
		SetUpdatedBy(cg.UpdatedBy).
		SetEnvironmentID(cg.EnvironmentID).
		SetMetadata(cg.Metadata)

	result, err := create.Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return nil, ierr.WithError(err).
				WithHint("A credit grant with these parameters already exists").
				WithReportableDetails(map[string]interface{}{
					"scope":           cg.Scope,
					"plan_id":         cg.PlanID,
					"subscription_id": cg.SubscriptionID,
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to create credit grant").
			WithReportableDetails(map[string]interface{}{
				"scope":           cg.Scope,
				"plan_id":         cg.PlanID,
				"subscription_id": cg.SubscriptionID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainCreditGrant.FromEnt(result), nil
}

func (r *creditGrantRepository) CreateBulk(ctx context.Context, creditGrants []*domainCreditGrant.CreditGrant) ([]*domainCreditGrant.CreditGrant, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "create_bulk", map[string]interface{}{
		"count":     len(creditGrants),
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(creditGrants) == 0 {
		return []*domainCreditGrant.CreditGrant{}, nil
	}

	// Validate all credit grants first
	for _, cg := range creditGrants {
		if err := cg.Validate(); err != nil {
			SetSpanError(span, err)
			return nil, err
		}
	}

	client := r.client.Writer(ctx)
	builders := make([]*ent.CreditGrantCreate, len(creditGrants))

	// Get environment ID from context
	environmentID := types.GetEnvironmentID(ctx)

	for i, cg := range creditGrants {
		// Set environment ID from context if not already set
		if cg.EnvironmentID == "" {
			cg.EnvironmentID = environmentID
		}

		builders[i] = client.CreditGrant.Create().
			SetID(cg.ID).
			SetName(cg.Name).
			SetScope(cg.Scope).
			SetCredits(cg.Credits).
			SetCadence(cg.Cadence).
			SetNillablePlanID(cg.PlanID).
			SetNillableSubscriptionID(cg.SubscriptionID).
			SetNillablePeriod(cg.Period).
			SetNillablePeriodCount(cg.PeriodCount).
			SetExpirationType(cg.ExpirationType).
			SetNillableExpirationDuration(cg.ExpirationDuration).
			SetNillableExpirationDurationUnit(cg.ExpirationDurationUnit).
			SetNillablePriority(cg.Priority).
			SetNillableStartDate(cg.StartDate).
			SetNillableEndDate(cg.EndDate).
			SetNillableCreditGrantAnchor(cg.CreditGrantAnchor).
			SetTenantID(cg.TenantID).
			SetStatus(string(cg.Status)).
			SetCreatedAt(cg.CreatedAt).
			SetUpdatedAt(cg.UpdatedAt).
			SetCreatedBy(cg.CreatedBy).
			SetUpdatedBy(cg.UpdatedBy).
			SetEnvironmentID(cg.EnvironmentID).
			SetMetadata(cg.Metadata)
	}

	results, err := client.CreditGrant.CreateBulk(builders...).Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsConstraintError(err) {
			return nil, ierr.WithError(err).
				WithHint("One or more credit grants with these parameters already exist").
				WithReportableDetails(map[string]interface{}{
					"count": len(creditGrants),
				}).
				Mark(ierr.ErrAlreadyExists)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to create credit grants in bulk").
			WithReportableDetails(map[string]interface{}{
				"count": len(creditGrants),
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainCreditGrant.FromEntList(results), nil
}

func (r *creditGrantRepository) Get(ctx context.Context, id string) (*domainCreditGrant.CreditGrant, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "get", map[string]interface{}{
		"creditgrant_id": id,
		"tenant_id":      types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	// Try to get from cache first
	if cachedCreditGrant := r.GetCache(ctx, id); cachedCreditGrant != nil {
		return cachedCreditGrant, nil
	}

	client := r.client.Reader(ctx)
	r.log.Debugw("getting credit grant",
		"creditgrant_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	result, err := client.CreditGrant.Query().
		Where(
			creditgrant.ID(id),
			creditgrant.TenantID(types.GetTenantID(ctx)),
			creditgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		Only(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Credit grant not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to get credit grant").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrDatabase)
	}

	creditGrantData := domainCreditGrant.FromEnt(result)
	r.SetCache(ctx, creditGrantData)
	return creditGrantData, nil
}

func (r *creditGrantRepository) List(ctx context.Context, filter *types.CreditGrantFilter) ([]*domainCreditGrant.CreditGrant, error) {
	if filter == nil {
		filter = &types.CreditGrantFilter{
			QueryFilter: types.NewDefaultQueryFilter(),
		}
	}

	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "list", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"plan_ids":  filter.PlanIDs,
	})
	defer FinishSpan(span)

	query := client.CreditGrant.Query()

	// Apply entity-specific filters
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	// Apply common query options
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)

	results, err := query.All(ctx)
	if err != nil {
		SetSpanError(span, err)
		return nil, ierr.WithError(err).
			WithHint("Failed to list credit grants").
			WithReportableDetails(map[string]interface{}{
				"plan_ids": filter.PlanIDs,
			}).
			Mark(ierr.ErrDatabase)
	}

	return domainCreditGrant.FromEntList(results), nil
}

func (r *creditGrantRepository) Count(ctx context.Context, filter *types.CreditGrantFilter) (int, error) {
	client := r.client.Reader(ctx)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "count", map[string]interface{}{
		"tenant_id": types.GetTenantID(ctx),
		"plan_ids":  filter.PlanIDs,
	})
	defer FinishSpan(span)

	query := client.CreditGrant.Query()

	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		SetSpanError(span, err)
		return 0, ierr.WithError(err).
			WithHint("Failed to count credit grants").
			WithReportableDetails(map[string]interface{}{
				"plan_ids": filter.PlanIDs,
			}).
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *creditGrantRepository) ListAll(ctx context.Context, filter *types.CreditGrantFilter) ([]*domainCreditGrant.CreditGrant, error) {
	if filter == nil {
		filter = types.NewNoLimitCreditGrantFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	if !filter.IsUnlimited() {
		filter.QueryFilter.Limit = nil
	}

	if err := filter.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid credit grant filter").
			WithReportableDetails(map[string]interface{}{
				"filter": filter,
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	return r.List(ctx, filter)
}

func (r *creditGrantRepository) Update(ctx context.Context, cg *domainCreditGrant.CreditGrant) (*domainCreditGrant.CreditGrant, error) {
	client := r.client.Writer(ctx)

	r.log.Debugw("updating credit grant",
		"creditgrant_id", cg.ID,
		"tenant_id", cg.TenantID,
	)

	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "update", map[string]interface{}{
		"creditgrant_id": cg.ID,
		"tenant_id":      cg.TenantID,
	})
	defer FinishSpan(span)

	update := client.CreditGrant.UpdateOneID(cg.ID).
		Where(
			creditgrant.TenantID(cg.TenantID),
			creditgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetName(cg.Name).
		SetScope(cg.Scope).
		SetStatus(string(cg.Status)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		SetMetadata(cg.Metadata)

	result, err := update.Save(ctx)
	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return nil, ierr.WithError(err).
				WithHint("Credit grant not found").
				WithReportableDetails(map[string]interface{}{"id": cg.ID}).
				Mark(ierr.ErrNotFound)
		}
		return nil, ierr.WithError(err).
			WithHint("Failed to update credit grant").
			WithReportableDetails(map[string]interface{}{"id": cg.ID}).
			Mark(ierr.ErrDatabase)
	}
	r.DeleteCache(ctx, cg.ID)
	return domainCreditGrant.FromEnt(result), nil
}

func (r *creditGrantRepository) Delete(ctx context.Context, id string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "delete", map[string]interface{}{
		"creditgrant_id": id,
		"tenant_id":      types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	client := r.client.Writer(ctx)

	r.log.Debugw("deleting credit grant",
		"creditgrant_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.CreditGrant.Update().
		Where(
			creditgrant.ID(id),
			creditgrant.TenantID(types.GetTenantID(ctx)),
			creditgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHint("Credit grant not found").
				WithReportableDetails(map[string]interface{}{"id": id}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete credit grant").
			WithReportableDetails(map[string]interface{}{"id": id}).
			Mark(ierr.ErrDatabase)
	}

	r.DeleteCache(ctx, id)
	return nil
}

func (r *creditGrantRepository) DeleteBulk(ctx context.Context, ids []string) error {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "delete_bulk", map[string]interface{}{
		"count":     len(ids),
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if len(ids) == 0 {
		return nil
	}

	r.log.Debugw("deleting credit grants in bulk", "count", len(ids))

	_, err := r.client.Writer(ctx).CreditGrant.Update().
		Where(
			creditgrant.IDIn(ids...),
			creditgrant.TenantID(types.GetTenantID(ctx)),
			creditgrant.EnvironmentID(types.GetEnvironmentID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		SetSpanError(span, err)
		return ierr.WithError(err).
			WithHint("Failed to delete credit grants in bulk").
			WithReportableDetails(map[string]interface{}{
				"count": len(ids),
			}).
			Mark(ierr.ErrDatabase)
	}

	// Clear cache for all deleted credit grants
	for _, id := range ids {
		r.DeleteCache(ctx, id)
	}

	return nil
}

// ListByPlanID retrieves all credit grants for the given plan ID
func (r *creditGrantRepository) GetByPlan(ctx context.Context, planID string) ([]*domainCreditGrant.CreditGrant, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "list_by_plan_id", map[string]interface{}{
		"plan_id":   planID,
		"tenant_id": types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if planID == "" {
		return []*domainCreditGrant.CreditGrant{}, nil
	}

	r.log.Debugw("listing credit grants by plan ID", "plan_id", planID)

	// Create a filter with plan ID
	filter := types.NewNoLimitCreditGrantFilter()
	filter.PlanIDs = []string{planID}
	filter.Status = lo.ToPtr(types.StatusPublished)
	filter.Scope = lo.ToPtr(types.CreditGrantScopePlan)

	// Use the existing List method
	return r.List(ctx, filter)
}

// GetBySubscription retrieves all credit grants for the given subscription ID
func (r *creditGrantRepository) GetBySubscription(ctx context.Context, subscriptionID string) ([]*domainCreditGrant.CreditGrant, error) {
	// Start a span for this repository operation
	span := StartRepositorySpan(ctx, "creditgrant", "list_by_subscription_id", map[string]interface{}{
		"subscription_id": subscriptionID,
		"tenant_id":       types.GetTenantID(ctx),
	})
	defer FinishSpan(span)

	if subscriptionID == "" {
		return []*domainCreditGrant.CreditGrant{}, nil
	}

	filter := types.NewNoLimitCreditGrantFilter()
	filter.SubscriptionIDs = []string{subscriptionID}
	filter.Status = lo.ToPtr(types.StatusPublished)
	filter.Scope = lo.ToPtr(types.CreditGrantScopeSubscription)

	return r.List(ctx, filter)
}

// CreditGrantQuery type alias for better readability
type CreditGrantQuery = *ent.CreditGrantQuery

// CreditGrantQueryOptions implements BaseQueryOptions for credit grant queries
type CreditGrantQueryOptions struct{}

func (o CreditGrantQueryOptions) ApplyTenantFilter(ctx context.Context, query CreditGrantQuery) CreditGrantQuery {
	return query.Where(creditgrant.TenantID(types.GetTenantID(ctx)))
}

func (o CreditGrantQueryOptions) ApplyEnvironmentFilter(ctx context.Context, query CreditGrantQuery) CreditGrantQuery {
	environmentID := types.GetEnvironmentID(ctx)
	if environmentID != "" {
		return query.Where(creditgrant.EnvironmentID(environmentID))
	}
	return query
}

func (o CreditGrantQueryOptions) ApplyStatusFilter(query CreditGrantQuery, status string) CreditGrantQuery {
	if status == "" {
		return query.Where(creditgrant.StatusNotIn(string(types.StatusDeleted)))
	}
	return query.Where(creditgrant.Status(status))
}

func (o CreditGrantQueryOptions) ApplySortFilter(query CreditGrantQuery, field string, order string) CreditGrantQuery {
	orderFunc := ent.Desc
	if order == "asc" {
		orderFunc = ent.Asc
	}
	return query.Order(orderFunc(o.GetFieldName(field)))
}

func (o CreditGrantQueryOptions) ApplyPaginationFilter(query CreditGrantQuery, limit int, offset int) CreditGrantQuery {
	query = query.Limit(limit)
	if offset > 0 {
		query = query.Offset(offset)
	}
	return query
}

func (o CreditGrantQueryOptions) GetFieldName(field string) string {
	switch field {
	case "created_at":
		return creditgrant.FieldCreatedAt
	case "updated_at":
		return creditgrant.FieldUpdatedAt
	default:
		return field
	}
}

func (o CreditGrantQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.CreditGrantFilter, query CreditGrantQuery) CreditGrantQuery {
	if f == nil {
		return query
	}

	// Apply plan IDs filter if specified
	if len(f.PlanIDs) > 0 {
		query = query.Where(creditgrant.PlanIDIn(f.PlanIDs...))
	}

	// Apply subscription IDs filter if specified
	if len(f.SubscriptionIDs) > 0 {
		query = query.Where(creditgrant.SubscriptionIDIn(f.SubscriptionIDs...))
	}

	// Apply scope filter if specified
	if f.Scope != nil {
		query = query.Where(creditgrant.Scope(lo.FromPtr(f.Scope)))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(creditgrant.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(creditgrant.CreatedAtLTE(*f.EndTime))
		}
	}

	return query
}

func (r *creditGrantRepository) SetCache(ctx context.Context, creditGrant *domainCreditGrant.CreditGrant) {
	span := cache.StartCacheSpan(ctx, "creditgrant", "set", map[string]interface{}{
		"creditgrant_id": creditGrant.ID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	// Using a constant for cache prefix
	cacheKey := cache.GenerateKey("credit_grant", tenantID, environmentID, creditGrant.ID)
	r.cache.Set(ctx, cacheKey, creditGrant, cache.ExpiryDefaultInMemory)
}

func (r *creditGrantRepository) GetCache(ctx context.Context, key string) *domainCreditGrant.CreditGrant {
	span := cache.StartCacheSpan(ctx, "creditgrant", "get", map[string]interface{}{
		"creditgrant_id": key,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	// Using a constant for cache prefix
	cacheKey := cache.GenerateKey("credit_grant", tenantID, environmentID, key)
	if value, found := r.cache.Get(ctx, cacheKey); found {
		return value.(*domainCreditGrant.CreditGrant)
	}
	return nil
}

func (r *creditGrantRepository) DeleteCache(ctx context.Context, creditGrantID string) {
	span := cache.StartCacheSpan(ctx, "creditgrant", "delete", map[string]interface{}{
		"creditgrant_id": creditGrantID,
	})
	defer cache.FinishSpan(span)

	tenantID := types.GetTenantID(ctx)
	environmentID := types.GetEnvironmentID(ctx)
	// Using a constant for cache prefix
	cacheKey := cache.GenerateKey("credit_grant", tenantID, environmentID, creditGrantID)
	r.cache.Delete(ctx, cacheKey)
}
