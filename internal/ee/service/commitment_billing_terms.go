package service

import (
	"context"
	"slices"
	"sort"
	"strings"

	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/api/dto"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	"github.com/flexprice/flexprice/internal/domain/commitment"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// CommitmentBillingTermsService exposes reusable collection rules through a fixed commitment owner.
type CommitmentBillingTermsService interface {
	Get(context.Context, string) (*dto.BillingTermsStateResponse, error)
	Preview(context.Context, string, dto.PreviewBillingTermsRequest) (*dto.BillingTermsPreviewResponse, error)
	Replace(context.Context, string, dto.ReplaceBillingTermsRequest) (*dto.BillingTermsStateResponse, error)
	ListItems(context.Context, string, dto.ListBillingScheduleItemsRequest) (*dto.ListBillingScheduleItemsResponse, error)
	GetItem(context.Context, string, string) (*dto.BillingScheduleItemResponse, error)
	History(context.Context, string, dto.ListBillingScheduleItemsRequest) (*dto.ListBillingScheduleItemsResponse, error)
	Trigger(context.Context, string, string, dto.TriggerBillingScheduleItemRequest) (*dto.TriggerBillingScheduleItemResponse, error)
}

type commitmentBillingTerms struct {
	params ServiceParams
	rules  domain.RuleRepository
	items  domain.ScheduleItemRepository
	audit  activity.Sink
}

// NewCommitmentBillingTermsService binds owner-scoped API operations to the generic collection core.
func NewCommitmentBillingTermsService(params ServiceParams, rules domain.RuleRepository, items domain.ScheduleItemRepository, audit *activity.Publisher) CommitmentBillingTermsService {
	s := &commitmentBillingTerms{params: params, rules: rules, items: items}
	if audit != nil {
		s.audit = audit
	}
	return s
}

func (s *commitmentBillingTerms) owner(ctx context.Context, id string) (*commitment.Commitment, error) {
	if ctx == nil || types.GetTenantID(ctx) == "" || types.GetEnvironmentID(ctx) == "" || strings.TrimSpace(id) == "" || len(id) > 50 {
		return nil, ierr.Validation("billing terms require a scoped commitment ID")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil || s.params.CommitmentRepo == nil || s.rules == nil || s.items == nil {
		return nil, ierr.NewError("billing terms are unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	c, err := s.params.CommitmentRepo.Get(types.WithForceWriter(ctx), id)
	if err != nil {
		return nil, err
	}
	if c == nil || c.ID != id || c.TenantID != types.GetTenantID(ctx) || c.EnvironmentID != types.GetEnvironmentID(ctx) ||
		(c.Status != types.StatusPublished && c.Status != types.StatusArchived) {
		return nil, ierr.NewError("commitment not found").Mark(ierr.ErrNotFound)
	}
	if !c.Binding || c.CollectionMethod == nil || *c.CollectionMethod != types.CommitmentCollectionBillingTerms {
		return nil, ierr.Validation("commitment does not use billing terms")
	}
	return c, nil
}

func (s *commitmentBillingTerms) ready(ctx context.Context, id string) (*commitment.Commitment, error) {
	c, err := s.owner(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.params.BillingTerms == nil {
		return nil, ierr.NewError("billing terms are unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	if s.params.DB == nil || s.params.BillingTermsStore == nil || s.params.Logger == nil || s.params.WebhookPublisher == nil {
		return nil, ierr.NewError("billing terms dependencies are unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	return c, nil
}

func commitmentBillingOwner(id string) domain.OwnerRef {
	return domain.OwnerRef{Type: types.BillingTermOwnerCommitment, ID: id}
}

func (s *commitmentBillingTerms) Get(ctx context.Context, id string) (*dto.BillingTermsStateResponse, error) {
	if _, err := s.ready(ctx, id); err != nil {
		return nil, err
	}
	state, err := s.params.BillingTerms.Get(ctx, commitmentBillingOwner(id))
	if err != nil {
		return nil, err
	}
	return s.stateResponse(ctx, id, state)
}

func (s *commitmentBillingTerms) Preview(ctx context.Context, id string, req dto.PreviewBillingTermsRequest) (*dto.BillingTermsPreviewResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if _, err := s.ready(ctx, id); err != nil {
		return nil, err
	}
	var response *dto.BillingTermsPreviewResponse
	err := s.params.DB.WithTx(ctx, func(tx context.Context) error {
		var err error
		response, err = s.preview(tx, id, req)
		return err
	})
	return response, err
}

func (s *commitmentBillingTerms) preview(ctx context.Context, id string, req dto.PreviewBillingTermsRequest) (*dto.BillingTermsPreviewResponse, error) {
	preview, err := s.params.BillingTerms.Preview(ctx, core.PreviewRequest{Owner: commitmentBillingOwner(id), Rows: req.BillingTerms.RuleInputs(), ReplaceInvoiceIDs: req.ReplaceInvoiceIDs})
	if err != nil {
		return nil, err
	}
	current := preview.Current()
	response := &dto.BillingTermsPreviewResponse{
		StateToken: current.StateToken(), CanApply: preview.CanApply(), Collection: current.Summary(),
		ArchiveRuleIDs: []string{}, ArchiveItemIDs: []string{}, RetainedInvoiceIDs: []string{},
		BlockingInvoiceIDs: append([]string{}, preview.BlockingInvoiceIDs()...), ConditionalReleasedPrincipal: preview.ConditionalReleasedPrincipal(),
		Items: []*dto.BillingScheduleItemResponse{}, Warnings: []string{},
	}
	rules := current.Rules()
	items := current.Items()
	retained := map[string]bool{}
	for _, item := range items {
		if item.InvoiceID != nil && item.ScheduleStatus == types.BillingScheduleInvoiced && !slices.Contains(req.ReplaceInvoiceIDs, *item.InvoiceID) {
			retained[*item.InvoiceID] = true
		}
		if !preview.Noop() && item.Status == types.StatusPublished {
			response.ArchiveItemIDs = append(response.ArchiveItemIDs, item.ID)
		}
	}
	for invoiceID := range retained {
		response.RetainedInvoiceIDs = append(response.RetainedInvoiceIDs, invoiceID)
	}
	sort.Strings(response.RetainedInvoiceIDs)
	if !preview.Noop() {
		for _, rule := range rules {
			response.ArchiveRuleIDs = append(response.ArchiveRuleIDs, rule.ID)
		}
		rules = preview.Plan().Rules()
		items = preview.Plan().Items()
		response.Collection, err = s.previewCollection(ctx, preview)
		if err != nil {
			return nil, err
		}
	}
	for _, item := range items {
		response.Items = append(response.Items, billingScheduleResponse(item))
	}
	if err := s.expandInvoices(ctx, response.Items); err != nil {
		return nil, err
	}
	if preview.CanApply() {
		response.Command = &dto.ReplaceBillingTermsRequest{ExpectedState: current.StateToken(), Reason: req.Reason, BillingTerms: billingRuleInputs(rules)}
	} else {
		response.Warnings = append(response.Warnings, "Correct the selected invoices, refresh the preview and review actual released principal before saving.")
	}
	return response, nil
}

func (s *commitmentBillingTerms) Replace(ctx context.Context, id string, req dto.ReplaceBillingTermsRequest) (*dto.BillingTermsStateResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	c, err := s.ready(ctx, id)
	if err != nil {
		return nil, err
	}
	var state core.State
	err = s.params.DB.WithTx(ctx, func(tx context.Context) error {
		var err error
		state, err = s.params.BillingTerms.Replace(tx, core.ReplaceRequest{Owner: commitmentBillingOwner(id), Rows: req.BillingTerms.RuleInputs(), ExpectedState: req.ExpectedState, Reason: req.Reason})
		if err != nil || state.Noop() {
			return err
		}
		return s.changed(tx, c, state, activity.CommitmentBillingTermsReplaced, req.Reason, "")
	})
	if err != nil {
		return nil, err
	}
	return s.stateResponse(ctx, id, state)
}

func (s *commitmentBillingTerms) Trigger(ctx context.Context, id, itemID string, req dto.TriggerBillingScheduleItemRequest) (*dto.TriggerBillingScheduleItemResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	c, err := s.ready(ctx, id)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(itemID) == "" || len(itemID) > 50 {
		return nil, ierr.Validation("billing schedule item ID is required")
	}
	var state core.State
	err = s.params.DB.WithTx(ctx, func(tx context.Context) error {
		var err error
		state, err = s.params.BillingTerms.Trigger(tx, core.TriggerRequest{Owner: commitmentBillingOwner(id), ItemID: itemID, ExpectedState: req.ExpectedState, BillOn: req.BillOn})
		if err != nil || state.Noop() {
			return err
		}
		return s.changed(tx, c, state, activity.CommitmentBillingItemTriggered, "", itemID)
	})
	if err != nil {
		return nil, err
	}
	for _, item := range state.Items() {
		if item.ID == itemID {
			response := billingScheduleResponse(item)
			if err := s.expandInvoices(ctx, []*dto.BillingScheduleItemResponse{response}); err != nil {
				return nil, err
			}
			return &dto.TriggerBillingScheduleItemResponse{Item: response, Collection: state.Summary(), StateToken: state.StateToken()}, nil
		}
	}
	return nil, ierr.NewError("triggered billing item was not returned").Mark(ierr.ErrDatabase)
}

func (s *commitmentBillingTerms) changed(ctx context.Context, c *commitment.Commitment, state core.State, action activity.Action, reason, itemID string) error {
	if s.params.Config != nil && s.params.Config.Activity.Enabled {
		payload := map[string]any{
			"id": c.ID, "name": c.Name, "customer_id": c.CustomerID, "subscription_id": c.SubscriptionID,
			"reason": reason, "state_token": state.StateToken(), "billing_terms": state.Rules(), "collection": state.Summary(),
		}
		if itemID != "" {
			for _, item := range state.Items() {
				if item.ID == itemID {
					payload["billing_schedule_item"] = item
					break
				}
			}
		}
		if err := activity.Record(ctx, activity.Registrations(), s.audit, action, c.ID, payload); err != nil {
			return err
		}
	}
	newCommitmentService(s.params).publishSystemEvent(ctx, types.WebhookEventCommitmentUpdated, c)
	return nil
}

func (s *commitmentBillingTerms) stateResponse(ctx context.Context, id string, state core.State) (*dto.BillingTermsStateResponse, error) {
	response := billingTermsStateResponse(ctx, id, state)
	if err := s.expandInvoices(ctx, response.BillingScheduleItems.Items); err != nil {
		return nil, err
	}
	return response, nil
}

func billingTermsStateResponse(ctx context.Context, id string, state core.State) *dto.BillingTermsStateResponse {
	rows := make([]*dto.BillingTermResponse, 0, len(state.Rules()))
	for _, rule := range state.Rules() {
		rows = append(rows, billingRuleResponse(rule))
	}
	items := make([]*domain.ScheduleItem, 0, len(state.Items()))
	for _, item := range state.Items() {
		if item.Status == types.StatusPublished {
			items = append(items, item)
		}
	}
	page := billingCurrentPage(ctx, id, dto.ListBillingScheduleItemsRequest{Limit: 50}, items, nil)
	return &dto.BillingTermsStateResponse{BillingTerms: dto.BillingTermsResponse{Rows: rows}, BillingScheduleItems: *page, Collection: state.Summary(), StateToken: state.StateToken()}
}

func billingRuleInputs(rules []*domain.Rule) dto.BillingTermsInput {
	rows := make([]dto.BillingTermInput, 0, len(rules))
	for _, rule := range rules {
		rows = append(rows, dto.BillingTermInput{
			Kind: rule.Kind, ValueType: rule.ValueType, Value: rule.Value, BillOn: rule.BillOn, OnTrigger: rule.OnTrigger,
			StartDate: rule.StartDate, EndDate: rule.EndDate, Interval: rule.Interval, IntervalCount: rule.IntervalCount,
			BillAt: rule.BillAt, Split: rule.Split, Description: rule.Description, Metadata: rule.Metadata,
		})
	}
	return dto.BillingTermsInput{Rows: rows}
}

func billingRuleResponse(rule *domain.Rule) *dto.BillingTermResponse {
	if rule == nil {
		return nil
	}
	copy := domain.NewRuleBuilder(rule).Build()
	return &dto.BillingTermResponse{Rule: copy, ID: copy.ID}
}

func billingScheduleResponse(item *domain.ScheduleItem) *dto.BillingScheduleItemResponse {
	if item == nil {
		return nil
	}
	copy := domain.NewScheduleItemBuilder(item).Build()
	return &dto.BillingScheduleItemResponse{ScheduleItem: copy, ID: copy.ID, BillOn: copy.BillOn}
}

func (s *commitmentBillingTerms) previewCollection(ctx context.Context, preview core.Preview) (*domain.CollectionSummary, error) {
	current := preview.Current()
	summary := current.Summary()
	summary.AssignedAmount = summary.AssignedAmount.Sub(preview.ConditionalReleasedPrincipal())
	if len(preview.BlockingInvoiceIDs()) > 0 {
		assigned := []*domain.ScheduleItem{}
		for _, item := range current.Items() {
			if item.ScheduleStatus == types.BillingScheduleInvoiced {
				assigned = append(assigned, item)
			}
		}
		bindings, err := s.params.BillingTermsStore.BindingStates(types.WithForceWriter(ctx), assigned)
		if err != nil {
			return nil, err
		}
		finalized := map[string]bool{}
		for _, binding := range bindings {
			finalized[binding.ItemID] = binding.InvoiceStatus == types.InvoiceStatusFinalized
		}
		for _, item := range current.Items() {
			if item.InvoiceID != nil && finalized[item.ID] && slices.Contains(preview.BlockingInvoiceIDs(), *item.InvoiceID) {
				summary.InvoicedAmount = summary.InvoicedAmount.Sub(item.Amount)
			}
		}
	}
	summary.ScheduledAmount = preview.Plan().Budget()
	summary.DueAmount = decimal.Zero
	summary.NextBillOn = nil
	for _, item := range preview.Plan().Items() {
		if item.BillOn == nil {
			continue
		}
		if !item.BillOn.After(summary.AsOf) {
			summary.DueAmount = summary.DueAmount.Add(item.Amount)
		}
		if summary.NextBillOn == nil || item.BillOn.Before(*summary.NextBillOn) {
			date := *item.BillOn
			summary.NextBillOn = &date
		}
	}
	summary.UnallocatedAmount = summary.TargetAmount.Sub(summary.AssignedAmount).Sub(summary.ScheduledAmount)
	summary.RequiresReplan = summary.UnallocatedAmount.IsPositive()
	if summary.DeferredReason != nil && *summary.DeferredReason == "requires_replan" {
		reason := current.Owner().DeferredReason
		summary.DeferredReason = nil
		if reason != "" {
			summary.DeferredReason = &reason
		}
	}
	return summary, nil
}
