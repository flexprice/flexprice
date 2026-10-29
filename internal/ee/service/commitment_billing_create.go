package service

import (
	"context"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

type preparedBillingCollection struct {
	owner *domain.OwnerSnapshot
	plan  core.Plan
}

type preparedCommitmentRepository struct {
	commitment.Repository
	commitment *commitment.Commitment
}

func (r *preparedCommitmentRepository) Get(ctx context.Context, id string) (*commitment.Commitment, error) {
	if r != nil && r.commitment != nil && r.commitment.ID == id && r.commitment.TenantID == types.GetTenantID(ctx) && r.commitment.EnvironmentID == types.GetEnvironmentID(ctx) {
		return commitment.NewCommitmentBuilder(r.commitment).Build(), nil
	}
	return nil, ierr.NewError("prepared commitment not found").Mark(ierr.ErrNotFound)
}

func (s *commitmentService) prepareCollection(ctx context.Context, c *commitment.Commitment, input dto.BillingTermsInput) (*preparedBillingCollection, error) {
	adapter := NewCommitmentTermsOwner(&preparedCommitmentRepository{commitment: c}, s.SubRepo)
	owner, err := adapter.Snapshot(ctx, commitmentBillingOwner(c.ID))
	if err != nil {
		return nil, err
	}
	if err := validateScheduleCurrency(ctx, s.CustomerRepo, owner.CustomerID, owner.Currency); err != nil {
		return nil, err
	}
	plan, err := core.NewPlanner().Plan(ctx, core.PlanRequest{Owner: *owner, Calendar: owner.Calendar, Rows: input.RuleInputs()})
	if err != nil {
		return nil, err
	}
	return &preparedBillingCollection{owner: owner, plan: plan}, nil
}

func (p *preparedBillingCollection) attach(response *dto.CommitmentResponse) {
	if p == nil || response == nil {
		return
	}
	rows := make([]*dto.BillingTermResponse, 0, len(p.plan.Rules()))
	for _, rule := range p.plan.Rules() {
		rows = append(rows, billingRuleResponse(rule))
	}
	items := make([]*dto.BillingScheduleItemResponse, 0, len(p.plan.Items()))
	summary := &domain.CollectionSummary{AsOf: time.Now().UTC(), Currency: p.owner.Currency, TargetAmount: p.owner.CollectionTarget, ScheduledAmount: p.plan.Budget()}
	if p.owner.DeferredReason != "" {
		summary.DeferredReason = lo.ToPtr(p.owner.DeferredReason)
	}
	for _, item := range p.plan.Items() {
		items = append(items, billingScheduleResponse(item))
		if item.BillOn == nil {
			continue
		}
		if !item.BillOn.After(summary.AsOf) {
			summary.DueAmount = summary.DueAmount.Add(item.Amount)
		}
		if summary.NextBillOn == nil || item.BillOn.Before(*summary.NextBillOn) {
			summary.NextBillOn = lo.ToPtr(*item.BillOn)
		}
	}
	response.BillingTerms = &dto.BillingTermsResponse{Rows: rows}
	response.BillingScheduleItems = &dto.ListBillingScheduleItemsResponse{Items: items, Pagination: dto.BillingPagination{Limit: len(items)}}
	response.Collection = summary
}

func (s *commitmentService) createdResponse(ctx context.Context, c *commitment.Commitment, cc *commitmentContext, state core.State) (*dto.CommitmentResponse, error) {
	response, err := s.response(ctx, c, cc)
	if err != nil {
		return nil, err
	}
	if !c.Binding || c.CollectionMethod == nil || *c.CollectionMethod != types.CommitmentCollectionBillingTerms {
		return response, nil
	}
	if state == nil {
		if s.BillingTerms == nil {
			return nil, ierr.NewError("billing terms are unavailable").Mark(ierr.ErrServiceUnavailable)
		}
		if s.BillingTermsStore == nil {
			return nil, ierr.NewError("billing terms store is unavailable").Mark(ierr.ErrServiceUnavailable)
		}
		err = s.DB.WithTx(ctx, func(tx context.Context) error {
			var err error
			tx, err = s.lockCollectionOwner(tx, c.SubscriptionID)
			if err != nil {
				return err
			}
			state, err = s.BillingTerms.Get(tx, commitmentBillingOwner(c.ID))
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	collection := billingTermsStateResponse(ctx, c.ID, state)
	if err := expandBillingInvoices(ctx, s.BillingTermsStore, collection.BillingScheduleItems.Items); err != nil {
		return nil, err
	}
	response.BillingTerms = &collection.BillingTerms
	response.BillingScheduleItems = &collection.BillingScheduleItems
	response.Collection = collection.Collection
	response.StateToken = collection.StateToken
	return response, nil
}
