package ent

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/plan"
	domainPlan "github.com/flexprice/flexprice/internal/domain/plan"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/types"
)

type planRepository struct {
	client    postgres.IClient
	log       *logger.Logger
	queryOpts PlanQueryOptions
}

func NewPlanRepository(client postgres.IClient, log *logger.Logger) domainPlan.Repository {
	return &planRepository{
		client:    client,
		log:       log,
		queryOpts: PlanQueryOptions{},
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

	*p = *domainPlan.FromEnt(plan)
	return nil
}

func (r *planRepository) Get(ctx context.Context, id string) (*domainPlan.Plan, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting plan", "plan_id", id)

	plan, err := client.Plan.Query().
		Where(
			plan.ID(id),
			plan.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
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

	return domainPlan.FromEnt(plan), nil
}

func (r *planRepository) List(ctx context.Context, filter *types.PlanFilter) ([]*domainPlan.Plan, error) {
	client := r.client.Querier(ctx)
	r.log.Debugw("listing plans",
		"tenant_id", types.GetTenantID(ctx),
		"limit", filter.GetLimit(),
		"offset", filter.GetOffset(),
	)

	query := client.Plan.Query()
	query = ApplyQueryOptions(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	plans, err := query.All(ctx)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Failed to list plans").
			Mark(ierr.ErrDatabase)
	}

	return domainPlan.FromEntList(plans), nil
}

func (r *planRepository) ListAll(ctx context.Context, filter *types.PlanFilter) ([]*domainPlan.Plan, error) {
	if filter == nil {
		filter = types.NewNoLimitPlanFilter()
	}

	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewNoLimitQueryFilter()
	}

	return r.List(ctx, filter)
}

func (r *planRepository) Count(ctx context.Context, filter *types.PlanFilter) (int, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("counting plans",
		"tenant_id", types.GetTenantID(ctx),
	)

	query := client.Plan.Query()
	query = ApplyBaseFilters(ctx, query, filter, r.queryOpts)
	query = r.queryOpts.applyEntityQueryOptions(ctx, filter, query)

	count, err := query.Count(ctx)
	if err != nil {
		return 0, ierr.WithError(err).
			WithHint("Failed to count plans").
			Mark(ierr.ErrDatabase)
	}

	return count, nil
}

func (r *planRepository) GetByLookupKey(ctx context.Context, lookupKey string) (*domainPlan.Plan, error) {
	r.log.Debugw("getting plan by lookup key", "lookup_key", lookupKey)

	plan, err := r.client.Querier(ctx).Plan.Query().
		Where(
			plan.LookupKey(lookupKey),
			plan.TenantID(types.GetTenantID(ctx)),
			plan.Status(string(types.StatusPublished)),
		).
		Only(ctx)
	if err != nil {
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

	return domainPlan.FromEnt(plan), nil
}

func (r *planRepository) Update(ctx context.Context, p *domainPlan.Plan) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("updating plan",
		"plan_id", p.ID,
		"tenant_id", p.TenantID,
	)

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
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Plan with ID %s was not found", p.ID).
				WithReportableDetails(map[string]any{
					"plan_id": p.ID,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to update plan").
			WithReportableDetails(map[string]any{
				"plan_id": p.ID,
			}).
			Mark(ierr.ErrDatabase)
	}

	return nil
}

func (r *planRepository) Delete(ctx context.Context, id string) error {
	client := r.client.Querier(ctx)

	r.log.Debugw("deleting plan",
		"plan_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Plan.Update().
		Where(
			plan.ID(id),
			plan.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusArchived)).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return ierr.WithError(err).
				WithHintf("Plan with ID %s was not found", id).
				WithReportableDetails(map[string]any{
					"plan_id": id,
				}).
				Mark(ierr.ErrNotFound)
		}
		return ierr.WithError(err).
			WithHint("Failed to delete plan").
			WithReportableDetails(map[string]any{
				"plan_id": id,
			}).
			Mark(ierr.ErrDatabase)
	}

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

func (o PlanQueryOptions) applyEntityQueryOptions(_ context.Context, f *types.PlanFilter, query PlanQuery) PlanQuery {
	if f == nil {
		return query
	}

	// Apply plan IDs filter if specified
	if len(f.PlanIDs) > 0 {
		query = query.Where(plan.IDIn(f.PlanIDs...))
	}

	// Apply time range filters if specified
	if f.TimeRangeFilter != nil {
		if f.StartTime != nil {
			query = query.Where(plan.CreatedAtGTE(*f.StartTime))
		}
		if f.EndTime != nil {
			query = query.Where(plan.CreatedAtLTE(*f.EndTime))
		}
	}

	return query
}
