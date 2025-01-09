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
	client postgres.IClient
	log    *logger.Logger
}

func NewPlanRepository(client postgres.IClient, log *logger.Logger) domainPlan.Repository {
	return &planRepository{
		client: client,
		log:    log,
	}
}

func (r *planRepository) Create(ctx context.Context, p *domainPlan.Plan) error {
	client := r.client.Querier(ctx)

	r.log.Debug("creating plan",
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

	r.log.Debug("getting plan",
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

func (r *planRepository) List(ctx context.Context, filter types.Filter) ([]*domainPlan.Plan, error) {
	client := r.client.Querier(ctx)

	r.log.Debug("listing plans",
		"tenant_id", types.GetTenantID(ctx),
		"limit", filter.Limit,
		"offset", filter.Offset,
	)

	plans, err := client.Plan.Query().
		Where(plan.TenantID(types.GetTenantID(ctx))).
		Order(ent.Desc(plan.FieldCreatedAt)).
		Limit(filter.Limit).
		Offset(filter.Offset).
		All(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to list plans: %w", err)
	}

	return domainPlan.FromEntList(plans), nil
}

func (r *planRepository) Update(ctx context.Context, p *domainPlan.Plan) error {
	client := r.client.Querier(ctx)

	r.log.Debug("updating plan",
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

	r.log.Debug("deleting plan",
		"plan_id", id,
		"tenant_id", types.GetTenantID(ctx),
	)

	_, err := client.Plan.Update().
		Where(
			plan.ID(id),
			plan.TenantID(types.GetTenantID(ctx)),
		).
		SetStatus(string(types.StatusDeleted)).
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
