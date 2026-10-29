package billingterms

import (
	"context"
	"slices"
	"strings"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InitializeRequest installs the initial full arrangement inside owner creation.
type InitializeRequest struct {
	Owner domain.OwnerRef
	Rows  []RuleInput
}

// PreviewRequest plans an edit without changing sources or invoices.
type PreviewRequest struct {
	Owner             domain.OwnerRef
	Rows              []RuleInput
	ReplaceInvoiceIDs []string
}

// ReplaceRequest supplies a complete replacement and its reviewed state.
type ReplaceRequest struct {
	Owner         domain.OwnerRef
	Rows          []RuleInput
	ExpectedState string
	Reason        string
}

// TriggerRequest resolves a manual item once; omitted dates use the first accepted time.
type TriggerRequest struct {
	Owner         domain.OwnerRef
	ItemID        string
	ExpectedState string
	BillOn        *time.Time
}

// State exposes detached current definitions and bounded retained invoice sources.
type State interface {
	Owner() *domain.OwnerSnapshot
	Rules() []*domain.Rule
	Items() []*domain.ScheduleItem
	Summary() *domain.CollectionSummary
	StateToken() string
	Noop() bool
}

// Preview separates a hypothetical invoice correction from an applicable plan.
type Preview interface {
	Current() State
	Plan() Plan
	Noop() bool
	CanApply() bool
	ConditionalReleasedPrincipal() decimal.Decimal
	BlockingInvoiceIDs() []string
}

// Service owns transactional source changes; invoice delivery is a separate caller.
type Service interface {
	Initialize(context.Context, InitializeRequest) (State, error)
	Get(context.Context, domain.OwnerRef) (State, error)
	Preview(context.Context, PreviewRequest) (Preview, error)
	Replace(context.Context, ReplaceRequest) (State, error)
	Trigger(context.Context, TriggerRequest) (State, error)
	// WithSettlement freezes an owner decision and replaces unclaimed charges atomically.
	WithSettlement(context.Context, SettlementRequest, func(context.Context, State) (bool, error)) (State, error)
	// ResolveFallback dates a still-current manual charge at its owner's deadline.
	ResolveFallback(context.Context, FallbackRequest) error
	// WithCollection requires the caller transaction and locks on any existing destination invoice.
	WithCollection(context.Context, CollectionRequest, func(context.Context, []*domain.ScheduleItem) ([]InvoiceSource, error)) error
	// WithInvoiceVoid requires the caller's transaction and invoice row lock.
	WithInvoiceVoid(context.Context, InvoiceVoidRequest, func(context.Context) error) error
}

type service struct {
	rules    domain.RuleRepository
	items    domain.ScheduleItemRepository
	store    domain.ExecutionStore
	resolver OwnerResolver
	planner  Planner
	now      func() time.Time
}

type preview struct {
	current  State
	plan     Plan
	noop     bool
	blocked  []string
	released decimal.Decimal
}

// NewService connects the owner-neutral planner and shared transactional ports.
func NewService(rules domain.RuleRepository, items domain.ScheduleItemRepository, store domain.ExecutionStore, resolver OwnerResolver) Service {
	if nilInterface(rules) || nilInterface(items) || nilInterface(store) || nilInterface(resolver) {
		return nil
	}
	return &service{rules: rules, items: items, store: store, resolver: resolver, planner: NewPlanner(), now: time.Now}
}

func (p *preview) Current() State {
	if p == nil {
		return nil
	}
	return p.current
}

func (p *preview) Plan() Plan {
	if p == nil {
		return nil
	}
	return p.plan
}

func (p *preview) Noop() bool { return p != nil && p.noop }

func (p *preview) CanApply() bool { return p != nil && len(p.blocked) == 0 }

func (p *preview) ConditionalReleasedPrincipal() decimal.Decimal {
	if p == nil {
		return decimal.Zero
	}
	return p.released
}

func (p *preview) BlockingInvoiceIDs() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.blocked...)
}

func (s *service) transaction(ctx context.Context, owner domain.OwnerRef, fn func(context.Context, *domain.OwnerSnapshot) error) error {
	if s == nil {
		return unavailable("billing terms service is unavailable")
	}
	if err := requestScope(ctx, owner); err != nil {
		return err
	}
	return s.store.WithTx(ctx, func(tx context.Context) error {
		discovered, err := s.snapshot(tx, owner)
		if err != nil {
			return err
		}
		if err := s.store.LockEligibility(tx, discovered.EligibilityScopes); err != nil {
			return err
		}
		if err := s.store.LockOwner(tx, owner); err != nil {
			return err
		}
		fresh, err := s.snapshot(tx, owner)
		if err != nil {
			return err
		}
		if !slices.Equal(discovered.EligibilityScopes, fresh.EligibilityScopes) {
			return ierr.NewError("billing eligibility changed; refresh the operation").
				WithReportableDetails(map[string]any{"reason": "billing_eligibility_changed"}).
				Mark(ierr.ErrVersionConflict)
		}
		return fn(tx, fresh)
	})
}

func (s *service) Get(ctx context.Context, owner domain.OwnerRef) (State, error) {
	var out *state
	err := s.transaction(ctx, owner, func(tx context.Context, snapshot *domain.OwnerSnapshot) error {
		var err error
		out, err = s.load(tx, snapshot, s.now().UTC())
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) Initialize(ctx context.Context, req InitializeRequest) (State, error) {
	var out *state
	err := s.transaction(ctx, req.Owner, func(tx context.Context, snapshot *domain.OwnerSnapshot) error {
		current, err := s.load(tx, snapshot, s.now().UTC())
		if err != nil {
			return err
		}
		if !current.owner.CanEdit {
			return ierr.Validation("billing owner does not permit billing term changes")
		}
		same, err := sameRules(req.Rows, current)
		if err != nil {
			return err
		}
		if same && !current.summary.RequiresReplan {
			current.noop = true
			out = current
			return nil
		}
		history, err := s.rules.List(tx, &domain.RuleFilter{Owner: &req.Owner, IncludeArchived: true, Limit: 1})
		if err != nil {
			return err
		}
		if len(history) != 0 || len(current.items) != 0 {
			return conflict(current, "billing terms are already initialized")
		}
		planned, err := s.plan(tx, current, req.Rows, false)
		if err != nil {
			return err
		}
		if err = s.persist(tx, planned); err != nil {
			return err
		}
		out, err = s.load(tx, snapshot, s.now().UTC())
		if err == nil && out.summary.RequiresReplan {
			return integrity("initialized billing schedule does not allocate its target")
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) Preview(ctx context.Context, req PreviewRequest) (Preview, error) {
	var out *preview
	err := s.transaction(ctx, req.Owner, func(tx context.Context, snapshot *domain.OwnerSnapshot) error {
		current, err := s.load(tx, snapshot, s.now().UTC())
		if err != nil {
			return err
		}
		if !current.owner.CanEdit {
			return ierr.Validation("billing owner does not permit billing term changes")
		}
		out = &preview{current: current, released: decimal.Zero}
		if len(req.ReplaceInvoiceIDs) == 0 {
			same, err := sameRules(req.Rows, current)
			if err != nil {
				return err
			}
			if same && !current.summary.RequiresReplan {
				out.noop = true
				return nil
			}
		}
		owner, released, err := previewOwner(current, req.ReplaceInvoiceIDs)
		if err != nil {
			return err
		}
		out.released = released
		out.blocked = append([]string(nil), req.ReplaceInvoiceIDs...)
		current = &state{owner: owner}
		out.plan, err = s.plan(tx, current, req.Rows, true)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func previewOwner(current *state, invoiceIDs []string) (*domain.OwnerSnapshot, decimal.Decimal, error) {
	if len(invoiceIDs) > MaxItems {
		return nil, decimal.Zero, ierr.Validation("too many replacement invoices")
	}
	selected := make(map[string]bool, len(invoiceIDs))
	for _, id := range invoiceIDs {
		if strings.TrimSpace(id) == "" || len(id) > 50 || selected[id] {
			return nil, decimal.Zero, ierr.Validation("replacement invoice ids must be nonempty, unique and fit 50 bytes")
		}
		selected[id] = true
	}
	owner := cloneOwner(current.owner)
	released := decimal.Zero
	found := make(map[string]bool, len(invoiceIDs))
	for _, item := range current.items {
		if item.ScheduleStatus != types.BillingScheduleInvoiced || item.InvoiceID == nil || !selected[*item.InvoiceID] {
			continue
		}
		released = released.Add(item.Amount)
		owner.AssignedAmount = owner.AssignedAmount.Sub(item.Amount)
		owner.RetainedItemCount--
		found[*item.InvoiceID] = true
	}
	for _, id := range invoiceIDs {
		if !found[id] {
			return nil, decimal.Zero, ierr.NewError("replacement invoice has no retained principal for owner").Mark(ierr.ErrNotFound)
		}
	}
	return owner, released, nil
}

func (s *service) Replace(ctx context.Context, req ReplaceRequest) (State, error) {
	var out *state
	err := s.transaction(ctx, req.Owner, func(tx context.Context, snapshot *domain.OwnerSnapshot) error {
		current, err := s.load(tx, snapshot, s.now().UTC())
		if err != nil {
			return err
		}
		if !current.owner.CanEdit {
			return ierr.Validation("billing owner does not permit billing term changes")
		}
		same, err := sameRules(req.Rows, current)
		if err != nil {
			return err
		}
		if same && !current.summary.RequiresReplan {
			current.noop = true
			out = current
			return nil
		}
		if req.ExpectedState == "" || req.ExpectedState != current.token {
			return conflict(current, "billing terms changed; refresh the preview")
		}
		planned, err := s.plan(tx, current, req.Rows, true)
		if err != nil {
			return err
		}
		for _, item := range current.items {
			if item.Status == types.StatusPublished && item.ScheduleStatus == types.BillingScheduleScheduled {
				changed, err := s.items.Cancel(tx, item.ID, types.BillingScheduleTermsAmended)
				if err != nil {
					return err
				}
				if !changed {
					return conflict(current, "billing item changed while replacing terms")
				}
			}
		}
		if err = s.items.Archive(tx, req.Owner); err != nil {
			return err
		}
		if err = s.rules.Archive(tx, req.Owner); err != nil {
			return err
		}
		if err = s.persist(tx, planned); err != nil {
			return err
		}
		out, err = s.load(tx, snapshot, s.now().UTC())
		if err == nil && out.summary.RequiresReplan {
			return integrity("replacement billing schedule does not allocate its target")
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) Trigger(ctx context.Context, req TriggerRequest) (State, error) {
	var out *state
	err := s.transaction(ctx, req.Owner, func(tx context.Context, snapshot *domain.OwnerSnapshot) error {
		current, err := s.load(tx, snapshot, s.now().UTC())
		if err != nil {
			return err
		}
		if !current.owner.CanEdit {
			return ierr.Validation("billing owner does not permit billing term changes")
		}
		var item *domain.ScheduleItem
		for _, candidate := range current.items {
			if candidate.ID == req.ItemID {
				item = candidate
				break
			}
		}
		if item == nil {
			historical, err := s.items.Get(tx, req.ItemID)
			if err != nil {
				return err
			}
			if historical != nil && historical.OwnerType == req.Owner.Type && historical.OwnerID == req.Owner.ID && historical.TenantID == current.owner.TenantID && historical.EnvironmentID == current.owner.EnvironmentID {
				return conflict(current, "archived or canceled billing schedule item cannot be triggered")
			}
			return ierr.NewError("billing schedule item not found").Mark(ierr.ErrNotFound)
		}
		if item.Status != types.StatusPublished || item.ScheduleStatus == types.BillingScheduleCanceled || item.BillTrigger != types.BillingScheduleManual {
			return conflict(current, "billing schedule item cannot be triggered")
		}
		if item.BillOn != nil {
			if req.BillOn == nil || item.BillOn.Equal(billingDate(*req.BillOn)) {
				current.noop = true
				out = current
				return nil
			}
			return conflict(current, "billing schedule item already has another date")
		}
		if req.ExpectedState == "" || req.ExpectedState != current.token {
			return conflict(current, "billing terms changed; refresh before triggering")
		}
		date := billingDate(s.now())
		if req.BillOn != nil {
			date = billingDate(*req.BillOn)
		}
		if !validDate(date) || date.Before(current.owner.StartDate) || date.After(current.owner.EndDate) {
			return ierr.Validation("trigger bill date must lie within the owner term")
		}
		changed, err := s.items.ResolveBillOn(tx, item.ID, date)
		if err != nil {
			return err
		}
		if !changed {
			return conflict(current, "billing schedule item changed while triggering")
		}
		out, err = s.load(tx, snapshot, s.now().UTC())
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *service) plan(ctx context.Context, current *state, rows []RuleInput, edit bool) (Plan, error) {
	return s.planner.Plan(ctx, PlanRequest{Owner: *current.owner, Rows: rows, Edit: edit, Calendar: current.owner.Calendar})
}

func (s *service) persist(ctx context.Context, planned Plan) error {
	for index, rule := range planned.Rules() {
		id := types.GenerateUUIDWithPrefix("bterm")
		rule = domain.NewRuleBuilder(rule).WithID(id).Build()
		if err := s.rules.Create(ctx, rule); err != nil {
			return err
		}
		for _, item := range planned.ItemsForRule(index) {
			item = domain.NewScheduleItemBuilder(item).WithID(types.GenerateUUIDWithPrefix("bsi")).WithBillingTermID(&id).Build()
			if err := s.items.Create(ctx, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func conflict(current *state, message string) error {
	return ierr.NewError(message).WithReportableDetails(map[string]any{"reason": "billing_state_changed", "state_token": current.token, "collection": current.summary}).Mark(ierr.ErrVersionConflict)
}
