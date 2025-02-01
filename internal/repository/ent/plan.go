package ent

import (
	"context"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/ent/plan"
	domainPlan "github.com/flexprice/flexprice/internal/domain/plan"
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
		"lookup_key", p.LookupKey,
	)

	plan, err := client.Plan.Create().
		SetID(p.ID).
		SetTenantID(p.TenantID).
		SetLookupKey(p.LookupKey).
		SetName(p.Name).
		SetDescription(p.Description).
		SetInvoiceCadence(string(p.InvoiceCadence)).
		SetTrialPeriod(p.TrialPeriod).
		SetStatus(string(p.Status)).
		SetCreatedAt(p.CreatedAt).
		SetUpdatedAt(p.UpdatedAt).
		SetCreatedBy(p.CreatedBy).
		SetUpdatedBy(p.UpdatedBy).
		Save(ctx)

	if err != nil {
		return fmt.Errorf("failed to create plan: %w", err)
	}

	*p = *domainPlan.FromEnt(plan)
	return nil
}

func (r *planRepository) Get(ctx context.Context, id string) (*domainPlan.Plan, error) {
	client := r.client.Querier(ctx)

	r.log.Debugw("getting plan",
		"plan_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	p, err := client.Plan.Query().
		Where(
			plan.ID(id),
			plan.TenantID(types.GetTenantID(ctx)),
		).
		Only(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return nil, fmt.Errorf("plan not found")
		}
		return nil, fmt.Errorf("failed to get plan: %w", err)
	}

	return domainPlan.FromEnt(p), nil
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
		return nil, fmt.Errorf("failed to list plans: %w", err)
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
		return 0, fmt.Errorf("failed to count plans: %w", err)
	}

	return count, nil
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
		SetInvoiceCadence(string(p.InvoiceCadence)).
		SetTrialPeriod(p.TrialPeriod).
		SetUpdatedAt(time.Now().UTC()).
		SetUpdatedBy(types.GetUserID(ctx)).
		Save(ctx)

	if err != nil {
		if ent.IsNotFound(err) {
			return fmt.Errorf("plan not found")
		}
		return fmt.Errorf("failed to update plan: %w", err)
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
			return fmt.Errorf("plan not found")
		}
		return fmt.Errorf("failed to delete plan: %w", err)
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

func (o PlanQueryOptions) applyEntityQueryOptions(ctx context.Context, filter *types.PlanFilter, query PlanQuery) PlanQuery {
	if filter == nil {
		return query
	}

	if len(filter.PlanIDs) > 0 {
		query = query.Where(plan.IDIn(filter.PlanIDs...))
	}

	return query
}
