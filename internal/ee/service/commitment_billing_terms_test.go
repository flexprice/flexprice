package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	core "github.com/flexprice/flexprice/internal/ee/service/billingterms"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/postgres"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type billingAPIRules struct {
	domain.RuleRepository
	rows []*domain.Rule
}

func (r *billingAPIRules) List(context.Context, *domain.RuleFilter) ([]*domain.Rule, error) {
	return r.rows, nil
}

type billingAPIItems struct {
	domain.ScheduleItemRepository
	rows    []*domain.ScheduleItem
	filters []*domain.ScheduleItemFilter
}

func (r *billingAPIItems) List(_ context.Context, filter *domain.ScheduleItemFilter) ([]*domain.ScheduleItem, error) {
	copy := *filter
	r.filters = append(r.filters, &copy)
	var out []*domain.ScheduleItem
	for _, item := range r.rows {
		if filter.After != nil && (item.CreatedAt.Before(filter.After.CreatedAt) || (item.CreatedAt.Equal(filter.After.CreatedAt) && item.ID <= filter.After.ID)) {
			continue
		}
		out = append(out, item)
		if len(out) == filter.Limit {
			break
		}
	}
	return out, nil
}
func (r *billingAPIItems) Get(_ context.Context, id string) (*domain.ScheduleItem, error) {
	for _, item := range r.rows {
		if item.ID == id {
			return item, nil
		}
	}
	return nil, errors.New("missing item")
}

type billingAPICore struct {
	core.Service
	state      core.State
	preview    core.Preview
	replaceErr error
	onReplace  func(context.Context, core.ReplaceRequest)
	onPreview  func(context.Context, core.PreviewRequest)
}

func (s *billingAPICore) Get(context.Context, domain.OwnerRef) (core.State, error) {
	return s.state, nil
}
func (s *billingAPICore) Replace(ctx context.Context, r core.ReplaceRequest) (core.State, error) {
	if s.onReplace != nil {
		s.onReplace(ctx, r)
	}
	return s.state, s.replaceErr
}
func (s *billingAPICore) Trigger(context.Context, core.TriggerRequest) (core.State, error) {
	return s.state, nil
}
func (s *billingAPICore) Preview(ctx context.Context, r core.PreviewRequest) (core.Preview, error) {
	if s.onPreview != nil {
		s.onPreview(ctx, r)
	}
	return s.preview, nil
}

type billingAPIState struct {
	owner   *domain.OwnerSnapshot
	rules   []*domain.Rule
	items   []*domain.ScheduleItem
	summary domain.CollectionSummary
	noop    bool
}

func (s *billingAPIState) Owner() *domain.OwnerSnapshot       { return s.owner }
func (s *billingAPIState) Rules() []*domain.Rule              { return s.rules }
func (s *billingAPIState) Items() []*domain.ScheduleItem      { return s.items }
func (s *billingAPIState) Summary() *domain.CollectionSummary { copy := s.summary; return &copy }
func (s *billingAPIState) StateToken() string                 { return "fresh_state" }
func (s *billingAPIState) Noop() bool                         { return s.noop }

type billingAPIPreview struct {
	core.Preview
	state    core.State
	plan     core.Plan
	noop     bool
	blocked  []string
	released decimal.Decimal
}

func (p *billingAPIPreview) Current() core.State                           { return p.state }
func (p *billingAPIPreview) Plan() core.Plan                               { return p.plan }
func (p *billingAPIPreview) Noop() bool                                    { return p.noop }
func (p *billingAPIPreview) CanApply() bool                                { return len(p.blocked) == 0 }
func (p *billingAPIPreview) BlockingInvoiceIDs() []string                  { return p.blocked }
func (p *billingAPIPreview) ConditionalReleasedPrincipal() decimal.Decimal { return p.released }

type billingAPIStore struct {
	domain.ExecutionStore
	bindings []domain.BindingState
}

func (s *billingAPIStore) BindingStates(context.Context, []*domain.ScheduleItem) ([]domain.BindingState, error) {
	return s.bindings, nil
}

type billingAPIDB struct {
	postgres.IClient
	beforeCommit func()
	failCommit   bool
}

func (d *billingAPIDB) WithTx(ctx context.Context, fn func(context.Context) error) error {
	tx := types.WithTxStore(types.WithPostCommitHooks(ctx))
	err := fn(tx)
	if d.beforeCommit != nil {
		d.beforeCommit()
	}
	if err != nil || d.failCommit {
		types.DiscardPostCommitHooks(tx)
		if err != nil {
			return err
		}
		return errors.New("commit failed")
	}
	types.RunPostCommitHooks(tx)
	return nil
}

type billingAPIAudit struct{ entries []activity.Entry }

func (s *billingAPIAudit) Publish(batch activity.Batch) {
	s.entries = append(s.entries, batch.Entries...)
}

type billingAPIFixture struct {
	owner     *commitmentTermsFixture
	service   *commitmentBillingTerms
	core      *billingAPICore
	state     *billingAPIState
	items     *billingAPIItems
	rules     *billingAPIRules
	db        *billingAPIDB
	audit     *billingAPIAudit
	publisher *testutil.InMemoryWebhookPublisher
}

func newBillingAPIFixture(t *testing.T) *billingAPIFixture {
	t.Helper()
	owner := newCommitmentTermsFixture(t)
	snapshot, err := owner.adapter.Snapshot(owner.ctx, owner.owner)
	require.NoError(t, err)
	cfg := &config.Configuration{Logging: config.LoggingConfig{Level: types.LogLevelInfo}}
	cfg.Activity.Enabled = true
	log, err := logger.NewLogger(cfg)
	require.NoError(t, err)
	state := &billingAPIState{owner: snapshot, summary: domain.CollectionSummary{AsOf: owner.commitment.StartDate, Currency: "usd", TargetAmount: decimal.NewFromInt(1200), ScheduledAmount: decimal.NewFromInt(1200)}}
	engine := &billingAPICore{state: state}
	items, rules := &billingAPIItems{}, &billingAPIRules{}
	db, audit, publisher := &billingAPIDB{}, &billingAPIAudit{}, testutil.NewInMemoryWebhookPublisher()
	service := &commitmentBillingTerms{params: ServiceParams{CommitmentRepo: owner.commitments, BillingTerms: engine, BillingTermsStore: &billingAPIStore{}, DB: db, Logger: log, Config: cfg, WebhookPublisher: publisher}, items: items, rules: rules, audit: audit}
	return &billingAPIFixture{owner: owner, service: service, core: engine, state: state, items: items, rules: rules, db: db, audit: audit, publisher: publisher}
}
func (f *billingAPIFixture) item(id string, date *time.Time) *domain.ScheduleItem {
	return &domain.ScheduleItem{ID: id, OwnerType: f.owner.owner.Type, OwnerID: f.owner.owner.ID, BillOn: date, EnvironmentID: types.GetEnvironmentID(f.owner.ctx), ScheduleStatus: types.BillingScheduleScheduled, BaseModel: types.GetDefaultBaseModel(f.owner.ctx)}
}
func (f *billingAPIFixture) rule(id string) *domain.Rule {
	return &domain.Rule{ID: id, OwnerType: f.owner.owner.Type, OwnerID: f.owner.owner.ID, Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: lo.ToPtr(decimal.NewFromInt(1200)), BillOn: lo.ToPtr(f.owner.commitment.StartDate), EnvironmentID: types.GetEnvironmentID(f.owner.ctx), BaseModel: types.GetDefaultBaseModel(f.owner.ctx)}
}
func billingAPIReplaceRequest() dto.ReplaceBillingTermsRequest {
	return dto.ReplaceBillingTermsRequest{ExpectedState: "reviewed_state", Reason: "Correct collection dates", BillingTerms: dto.BillingTermsInput{Rows: []dto.BillingTermInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: true}}}}
}

func TestCommitmentBillingAPI_ReplacementCommitBoundaryAndReplay(t *testing.T) {
	for _, tc := range []struct {
		name       string
		noop, fail bool
		coreErr    error
		events     int
	}{{name: "changed", events: 1}, {name: "identical replay", noop: true}, {name: "commit failure", fail: true}, {name: "stale state", coreErr: errors.New("stale state")}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBillingAPIFixture(t)
			f.state.noop = tc.noop
			f.db.failCommit = tc.fail
			f.core.replaceErr = tc.coreErr
			f.db.beforeCommit = func() { require.Empty(t, f.audit.entries); require.Empty(t, f.publisher.Events()) }
			request := billingAPIReplaceRequest()
			f.core.onReplace = func(ctx context.Context, r core.ReplaceRequest) {
				require.Equal(t, f.owner.owner, r.Owner)
				require.Equal(t, request.Reason, r.Reason)
				require.Equal(t, request.ExpectedState, r.ExpectedState)
			}
			response, err := f.service.Replace(f.owner.ctx, f.owner.owner.ID, request)
			if tc.fail || tc.coreErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, "fresh_state", response.StateToken)
			}
			require.Len(t, f.audit.entries, tc.events)
			require.Len(t, f.publisher.Events(), tc.events)
			if tc.events > 0 {
				var payload map[string]any
				require.NoError(t, json.Unmarshal(f.audit.entries[0].State, &payload))
				require.Equal(t, request.Reason, payload["reason"])
			}
		})
	}
}

func TestCommitmentBillingAPI_CurrentPagingAndCursorScope(t *testing.T) {
	f := newBillingAPIFixture(t)
	start := f.owner.commitment.StartDate
	f.items.rows = []*domain.ScheduleItem{f.item("manual", nil), f.item("later", lo.ToPtr(start.AddDate(0, 1, 0))), f.item("first_b", &start), f.item("first_a", &start)}
	request := dto.ListBillingScheduleItemsRequest{Limit: 2}
	page, err := f.service.ListItems(f.owner.ctx, f.owner.owner.ID, request)
	require.NoError(t, err)
	require.Equal(t, "first_a", page.Items[0].ID)
	require.Equal(t, "first_b", page.Items[1].ID)
	require.True(t, page.Pagination.HasMore)
	request.Cursor = *page.Pagination.NextCursor
	page, err = f.service.ListItems(f.owner.ctx, f.owner.owner.ID, request)
	require.NoError(t, err)
	require.Equal(t, "later", page.Items[0].ID)
	require.Equal(t, "manual", page.Items[1].ID)
	require.False(t, page.Pagination.HasMore)
	request.IncludeArchived = true
	_, err = f.service.ListItems(f.owner.ctx, f.owner.owner.ID, request)
	require.Error(t, err)
	request.IncludeArchived = false
	request.ScheduleStatus = types.BillingScheduleInvoiced
	_, err = f.service.ListItems(f.owner.ctx, f.owner.owner.ID, request)
	require.Error(t, err)
	_, err = decodeBillingPage(types.SetEnvironmentID(f.owner.ctx, "other"), f.owner.owner.ID, dto.ListBillingScheduleItemsRequest{Cursor: request.Cursor})
	require.Error(t, err)
}

func TestCommitmentBillingAPI_ArchivedHistoryAndExactSources(t *testing.T) {
	f := newBillingAPIFixture(t)
	f.owner.commitment.CommitmentStatus = types.CommitmentStatusTerminated
	item := f.item("old_item", nil)
	item.Status = types.StatusArchived
	item.BillingTermID = lo.ToPtr("old_rule")
	rule := f.rule("old_rule")
	rule.Status = types.StatusArchived
	f.items.rows = []*domain.ScheduleItem{item}
	f.rules.rows = []*domain.Rule{rule}
	response, err := f.service.GetItem(f.owner.ctx, f.owner.owner.ID, item.ID)
	require.NoError(t, err)
	require.Equal(t, rule.ID, response.BillingTerm.ID)
	require.Equal(t, types.StatusArchived, response.Status)
	page, err := f.service.History(f.owner.ctx, f.owner.owner.ID, dto.ListBillingScheduleItemsRequest{})
	require.NoError(t, err)
	require.True(t, f.items.filters[len(f.items.filters)-1].IncludeArchived)
	require.Equal(t, rule.ID, page.Items[0].BillingTerm.ID)
	rule.EnvironmentID = "other"
	_, err = f.service.GetItem(f.owner.ctx, f.owner.owner.ID, item.ID)
	require.Error(t, err)
}

func TestCommitmentBillingAPI_RejectsForeignScopeAndUnsupportedOwner(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*billingAPIFixture)
	}{
		{"owner environment", func(f *billingAPIFixture) { f.owner.commitment.EnvironmentID = "foreign" }},
		{"item owner", func(f *billingAPIFixture) { f.items.rows[0].OwnerID = "foreign" }},
		{"item tenant", func(f *billingAPIFixture) { f.items.rows[0].TenantID = "foreign" }},
		{"legacy collection", func(f *billingAPIFixture) { f.owner.commitment.CollectionMethod = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newBillingAPIFixture(t)
			f.items.rows = []*domain.ScheduleItem{f.item("item", nil)}
			tc.change(f)
			_, err := f.service.GetItem(f.owner.ctx, f.owner.owner.ID, "item")
			require.Error(t, err)
		})
	}
}

func TestCommitmentBillingAPI_PreviewNoopProducesExecutableCurrentCommand(t *testing.T) {
	f := newBillingAPIFixture(t)
	f.state.rules = []*domain.Rule{f.rule("current_rule")}
	f.state.items = []*domain.ScheduleItem{f.item("current_item", nil)}
	f.core.preview = &billingAPIPreview{state: f.state, noop: true}
	response, err := f.service.Preview(f.owner.ctx, f.owner.owner.ID, dto.PreviewBillingTermsRequest{Reason: "Same arrangement", BillingTerms: billingAPIReplaceRequest().BillingTerms})
	require.NoError(t, err)
	require.True(t, response.CanApply)
	require.NotNil(t, response.Command)
	require.Equal(t, "fresh_state", response.Command.ExpectedState)
	require.Equal(t, "1200", response.Command.BillingTerms.Rows[0].Value.String())
	require.Empty(t, response.ArchiveItemIDs)
	require.Empty(t, response.ArchiveRuleIDs)
	require.Empty(t, f.audit.entries)
	require.Empty(t, f.publisher.Events())
}

func TestCommitmentBillingAPI_ConditionalPreviewCannotBeSaved(t *testing.T) {
	f := newBillingAPIFixture(t)
	invoiceID := "inv_original"
	item := f.item("assigned", lo.ToPtr(f.owner.commitment.StartDate))
	item.ScheduleStatus = types.BillingScheduleInvoiced
	item.InvoiceID = &invoiceID
	item.InvoiceLineItemID = lo.ToPtr("line")
	item.Amount = decimal.NewFromInt(300)
	f.state.items = []*domain.ScheduleItem{item}
	f.state.rules = []*domain.Rule{f.rule("original_rule")}
	f.state.summary.AssignedAmount = decimal.NewFromInt(300)
	f.state.summary.InvoicedAmount = decimal.NewFromInt(300)
	f.state.summary.ScheduledAmount = decimal.NewFromInt(900)
	f.service.params.BillingTermsStore = &billingAPIStore{bindings: []domain.BindingState{{ItemID: item.ID, InvoiceStatus: types.InvoiceStatusFinalized}}}
	plan, err := core.NewPlanner().Plan(f.owner.ctx, core.PlanRequest{Owner: *f.state.owner, Edit: true, Rows: []core.RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: lo.ToPtr(f.owner.commitment.StartDate)}}})
	require.NoError(t, err)
	f.core.preview = &billingAPIPreview{state: f.state, plan: plan, blocked: []string{invoiceID}, released: decimal.NewFromInt(300)}
	response, err := f.service.Preview(f.owner.ctx, f.owner.owner.ID, dto.PreviewBillingTermsRequest{Reason: "Correct initial setup", BillingTerms: billingAPIReplaceRequest().BillingTerms, ReplaceInvoiceIDs: []string{invoiceID}})
	require.NoError(t, err)
	require.False(t, response.CanApply)
	require.Nil(t, response.Command)
	require.Equal(t, []string{invoiceID}, response.BlockingInvoiceIDs)
	require.True(t, response.Collection.AssignedAmount.IsZero())
	require.True(t, response.Collection.InvoicedAmount.IsZero())
	require.True(t, response.Collection.ScheduledAmount.Equal(decimal.NewFromInt(1200)))
	require.Empty(t, response.RetainedInvoiceIDs)
	require.NotEmpty(t, response.Warnings)
	require.Empty(t, response.Items[0].ID)
	require.Equal(t, []string{item.ID}, response.ArchiveItemIDs)
	require.Empty(t, f.audit.entries)
}

func TestCommitmentBillingAPI_TriggerRecordsResolvedSourceOnlyOnce(t *testing.T) {
	f := newBillingAPIFixture(t)
	item := f.item("milestone", lo.ToPtr(f.owner.commitment.StartDate))
	f.state.items = []*domain.ScheduleItem{item}
	request := dto.TriggerBillingScheduleItemRequest{ExpectedState: "reviewed_state"}
	response, err := f.service.Trigger(f.owner.ctx, f.owner.owner.ID, item.ID, request)
	require.NoError(t, err)
	require.Equal(t, item.ID, response.Item.ID)
	require.Len(t, f.audit.entries, 1)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(f.audit.entries[0].State, &payload))
	require.Equal(t, item.ID, payload["billing_schedule_item"].(map[string]any)["id"])
	f.state.noop = true
	_, err = f.service.Trigger(f.owner.ctx, f.owner.owner.ID, item.ID, request)
	require.NoError(t, err)
	require.Len(t, f.audit.entries, 1)
	require.Len(t, f.publisher.Events(), 1)
}

func TestCommitmentBillingAPI_InvoiceStatusDrilldown(t *testing.T) {
	for _, status := range []types.InvoiceStatus{types.InvoiceStatusDraft, types.InvoiceStatusFinalized, types.InvoiceStatusVoided} {
		t.Run(string(status), func(t *testing.T) {
			f := newBillingAPIFixture(t)
			item := f.item("bound", nil)
			item.InvoiceID, item.InvoiceLineItemID = lo.ToPtr("inv_source"), lo.ToPtr("ili_source")
			item.Status = types.StatusArchived
			item.ScheduleStatus = types.BillingScheduleInvoiced
			if status == types.InvoiceStatusVoided {
				item.ScheduleStatus = types.BillingScheduleCanceled
			}
			f.items.rows = []*domain.ScheduleItem{item}
			binding := domain.BindingState{ItemID: item.ID, InvoiceID: *item.InvoiceID, InvoiceLineItemID: *item.InvoiceLineItemID, TenantID: item.TenantID, EnvironmentID: item.EnvironmentID, InvoiceStatus: status}
			f.service.params.BillingTermsStore = &billingAPIStore{bindings: []domain.BindingState{binding}}
			response, err := f.service.GetItem(f.owner.ctx, f.owner.owner.ID, item.ID)
			require.NoError(t, err)
			require.Equal(t, status, *response.InvoiceStatus)
			page, err := f.service.History(f.owner.ctx, f.owner.owner.ID, dto.ListBillingScheduleItemsRequest{})
			require.NoError(t, err)
			require.Equal(t, status, *page.Items[0].InvoiceStatus)
			f.service.params.BillingTermsStore = &billingAPIStore{}
			_, err = f.service.GetItem(f.owner.ctx, f.owner.owner.ID, item.ID)
			require.ErrorContains(t, err, "source is missing")
			binding.EnvironmentID = "different"
			f.service.params.BillingTermsStore = &billingAPIStore{bindings: []domain.BindingState{binding}}
			_, err = f.service.GetItem(f.owner.ctx, f.owner.owner.ID, item.ID)
			require.ErrorContains(t, err, "source is inconsistent")
		})
	}
}
