package billingterms

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	domain "github.com/flexprice/flexprice/internal/domain/billingterms"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type memoryExecution struct {
	mu                 sync.Mutex
	owner              domain.OwnerSnapshot
	rules              map[string]*domain.Rule
	items              map[string]*domain.ScheduleItem
	bindings           map[string]domain.BindingState
	failCreate         bool
	missingBinding     bool
	wrongBinding       bool
	clock              int64
	lockOrder          []string
	eligibilityLocks   [][]domain.EligibilityScope
	eligibilityFailure error
	afterEligibility   func()
}
type memoryRules struct{ db *memoryExecution }
type memoryItems struct{ db *memoryExecution }
type memoryOwner struct{ db *memoryExecution }
type memoryTransaction struct{}

func newCore(t *testing.T) (context.Context, Service, *memoryExecution) {
	t.Helper()
	request := testRequest(t)
	owner := request.Owner
	owner.Calendar = request.Calendar
	db := &memoryExecution{owner: owner, rules: map[string]*domain.Rule{}, items: map[string]*domain.ScheduleItem{}, bindings: map[string]domain.BindingState{}}
	resolver, err := NewOwnerResolver(&memoryOwner{db: db})
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), types.CtxTenantID, owner.TenantID)
	ctx = context.WithValue(ctx, types.CtxEnvironmentID, owner.EnvironmentID)
	core := NewService(&memoryRules{db}, &memoryItems{db}, db, resolver)
	core.(*service).now = func() time.Time { return time.Date(2027, 5, 10, 0, 0, 0, 0, time.UTC) }
	return ctx, core, db
}
func (m *memoryOwner) Type() types.BillingTermOwnerType { return m.db.owner.Owner.Type }
func (m *memoryOwner) Snapshot(_ context.Context, owner domain.OwnerRef) (*domain.OwnerSnapshot, error) {
	m.db.lockOrder = append(m.db.lockOrder, "snapshot")
	if owner != m.db.owner.Owner {
		return nil, ierr.NewError("missing owner").Mark(ierr.ErrNotFound)
	}
	return cloneOwner(&m.db.owner), nil
}
func (m *memoryExecution) WithTx(ctx context.Context, fn func(context.Context) error) error {
	if ctx.Value(memoryTransaction{}) == m {
		return fn(ctx)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	owner := cloneOwner(&m.owner)
	rules := map[string]*domain.Rule{}
	for id, row := range m.rules {
		rules[id] = domain.NewRuleBuilder(row).Build()
	}
	items := map[string]*domain.ScheduleItem{}
	for id, row := range m.items {
		items[id] = domain.NewScheduleItemBuilder(row).Build()
	}
	if err := fn(context.WithValue(ctx, memoryTransaction{}, m)); err != nil {
		m.owner = *owner
		m.rules = rules
		m.items = items
		return err
	}
	return nil
}
func (m *memoryExecution) LockOwner(ctx context.Context, owner domain.OwnerRef) error {
	if ctx.Value(memoryTransaction{}) != m {
		return errors.New("missing transaction")
	}
	if owner != m.owner.Owner {
		return errors.New("foreign owner")
	}
	m.lockOrder = append(m.lockOrder, "owner")
	return nil
}

func (m *memoryExecution) LockEligibility(ctx context.Context, scopes []domain.EligibilityScope) error {
	if !m.InTransaction(ctx) {
		return errors.New("missing transaction")
	}
	m.lockOrder = append(m.lockOrder, "eligibility")
	m.eligibilityLocks = append(m.eligibilityLocks, append([]domain.EligibilityScope{}, scopes...))
	if m.eligibilityFailure != nil {
		return m.eligibilityFailure
	}
	if m.afterEligibility != nil {
		m.afterEligibility()
	}
	return nil
}

func (m *memoryExecution) InTransaction(ctx context.Context) bool {
	return ctx != nil && ctx.Value(memoryTransaction{}) == m
}
func (m *memoryExecution) BindingStates(_ context.Context, items []*domain.ScheduleItem) ([]domain.BindingState, error) {
	out := []domain.BindingState{}
	for _, item := range items {
		if m.missingBinding {
			continue
		}
		row := m.bindings[item.ID]
		if m.wrongBinding {
			row.InvoiceLineItemID = "wrong-line"
		}
		out = append(out, row)
	}
	return out, nil
}
func (m *memoryExecution) stamp() time.Time { m.clock++; return time.Unix(m.clock, 0).UTC() }
func (m *memoryRules) Create(_ context.Context, row *domain.Rule) error {
	m.db.rules[row.ID] = domain.NewRuleBuilder(row).Build()
	m.db.rules[row.ID].CreatedAt = m.db.stamp()
	return nil
}
func (m *memoryRules) Get(_ context.Context, id string) (*domain.Rule, error) {
	row, ok := m.db.rules[id]
	if !ok {
		return nil, ierr.NewError("missing rule").Mark(ierr.ErrNotFound)
	}
	return domain.NewRuleBuilder(row).Build(), nil
}
func (m *memoryRules) List(_ context.Context, f *domain.RuleFilter) ([]*domain.Rule, error) {
	out := []*domain.Rule{}
	for _, row := range m.db.rules {
		if f.Owner != nil && (row.OwnerType != f.Owner.Type || row.OwnerID != f.Owner.ID) {
			continue
		}
		if !f.IncludeArchived && row.Status != types.StatusPublished {
			continue
		}
		if f.After != nil && !after(row.CreatedAt, row.ID, f.After) {
			continue
		}
		out = append(out, domain.NewRuleBuilder(row).Build())
	}
	sort.Slice(out, func(i, j int) bool { return order(out[i].CreatedAt, out[i].ID, out[j].CreatedAt, out[j].ID) })
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}
func (m *memoryRules) Archive(_ context.Context, owner domain.OwnerRef) error {
	for id, row := range m.db.rules {
		if row.OwnerType == owner.Type && row.OwnerID == owner.ID {
			m.db.rules[id] = domain.NewRuleBuilder(row).WithStatus(types.StatusArchived).Build()
		}
	}
	return nil
}
func (m *memoryItems) Create(_ context.Context, row *domain.ScheduleItem) error {
	if m.db.failCreate {
		return errors.New("injected item failure")
	}
	m.db.items[row.ID] = domain.NewScheduleItemBuilder(row).Build()
	m.db.items[row.ID].CreatedAt = m.db.stamp()
	return nil
}
func (m *memoryItems) Get(_ context.Context, id string) (*domain.ScheduleItem, error) {
	row, ok := m.db.items[id]
	if !ok {
		return nil, ierr.NewError("missing item").Mark(ierr.ErrNotFound)
	}
	return domain.NewScheduleItemBuilder(row).Build(), nil
}
func (m *memoryItems) List(_ context.Context, f *domain.ScheduleItemFilter) ([]*domain.ScheduleItem, error) {
	out := []*domain.ScheduleItem{}
	for _, row := range m.db.items {
		if len(f.InvoiceIDs) > 0 {
			matches := false
			for _, id := range f.InvoiceIDs {
				matches = matches || row.InvoiceID != nil && *row.InvoiceID == id
			}
			if !matches {
				continue
			}
		}
		if f.Owner != nil && (row.OwnerType != f.Owner.Type || row.OwnerID != f.Owner.ID) {
			continue
		}
		if !f.IncludeArchived && row.Status != types.StatusPublished {
			continue
		}
		if f.RelevantOnly && row.Status != types.StatusPublished && row.ScheduleStatus != types.BillingScheduleInvoiced {
			continue
		}
		if f.After != nil && !after(row.CreatedAt, row.ID, f.After) {
			continue
		}
		out = append(out, domain.NewScheduleItemBuilder(row).Build())
	}
	sort.Slice(out, func(i, j int) bool { return order(out[i].CreatedAt, out[i].ID, out[j].CreatedAt, out[j].ID) })
	if len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}
func (m *memoryItems) ResolveBillOn(_ context.Context, id string, date time.Time) (bool, error) {
	row := m.db.items[id]
	if row == nil || row.Status != types.StatusPublished || row.BillOn != nil || row.ScheduleStatus != types.BillingScheduleScheduled {
		return false, nil
	}
	m.db.items[id] = domain.NewScheduleItemBuilder(row).WithBillOn(&date).Build()
	return true, nil
}
func (m *memoryItems) Claim(_ context.Context, id, invoiceID, lineID string) (bool, error) {
	row := m.db.items[id]
	if row == nil || row.Status != types.StatusPublished || row.ScheduleStatus != types.BillingScheduleScheduled || row.InvoiceID != nil {
		return false, nil
	}
	m.db.items[id] = domain.NewScheduleItemBuilder(row).WithScheduleStatus(types.BillingScheduleInvoiced).
		WithInvoiceID(&invoiceID).WithInvoiceLineItemID(&lineID).Build()
	return true, nil
}
func (m *memoryItems) Cancel(_ context.Context, id string, reason types.BillingScheduleCancellationReason) (bool, error) {
	row := m.db.items[id]
	if row == nil || row.ScheduleStatus == types.BillingScheduleCanceled {
		return false, nil
	}
	m.db.items[id] = domain.NewScheduleItemBuilder(row).WithStatus(types.StatusArchived).WithScheduleStatus(types.BillingScheduleCanceled).WithCancellationReason(&reason).Build()
	return true, nil
}
func (m *memoryItems) Archive(_ context.Context, owner domain.OwnerRef) error {
	for id, row := range m.db.items {
		if row.OwnerType == owner.Type && row.OwnerID == owner.ID {
			m.db.items[id] = domain.NewScheduleItemBuilder(row).WithStatus(types.StatusArchived).Build()
		}
	}
	return nil
}
func after(date time.Time, id string, cursor *domain.Cursor) bool {
	return date.After(cursor.CreatedAt) || (date.Equal(cursor.CreatedAt) && id > cursor.ID)
}
func order(a time.Time, id string, b time.Time, other string) bool {
	return a.Before(b) || (a.Equal(b) && id < other)
}
func claimMemory(db *memoryExecution, item *domain.ScheduleItem, status types.InvoiceStatus) {
	invoiceID := "invoice-" + item.ID
	lineID := "line-" + item.ID
	db.items[item.ID] = domain.NewScheduleItemBuilder(db.items[item.ID]).WithScheduleStatus(types.BillingScheduleInvoiced).WithInvoiceID(&invoiceID).WithInvoiceLineItemID(&lineID).Build()
	db.bindings[item.ID] = domain.BindingState{ItemID: item.ID, InvoiceID: invoiceID, InvoiceLineItemID: lineID, TenantID: db.owner.TenantID, EnvironmentID: db.owner.EnvironmentID, InvoiceStatus: status}
}
func annualRows() []RuleInput {
	return []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: ptr(decimal.NewFromInt(300))}, recurringRule()}
}
func replacementRows(owner domain.OwnerSnapshot) []RuleInput {
	row := recurringRule()
	row.StartDate = ptr(owner.StartDate.AddDate(0, 4, 0))
	row.EndDate = ptr(owner.EndDate)
	row.IntervalCount = ptr(2)
	return []RuleInput{row}
}
func sourceIDs(state State) []string {
	out := []string{}
	for _, item := range state.Items() {
		out = append(out, item.ID)
	}
	sort.Strings(out)
	return out
}

func TestCoreRetainedClaimsAndNoop(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	require.Len(t, initial.Items(), 13)
	require.Equal(t, "1200", initial.Summary().ScheduledAmount.String())
	require.False(t, initial.Summary().RequiresReplan)
	for index, item := range initial.Items()[:5] {
		status := types.InvoiceStatusFinalized
		if index == 0 {
			status = types.InvoiceStatusDraft
		}
		claimMemory(db, item, status)
	}
	current, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, "600", current.Summary().AssignedAmount.String())
	require.Equal(t, "300", current.Summary().InvoicedAmount.String())
	require.NotEqual(t, initial.StateToken(), current.StateToken())
	rows := annualRows()
	rows[0], rows[1] = rows[1], rows[0]
	retried, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: rows, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.True(t, retried.Noop())
	require.Equal(t, sourceIDs(current), sourceIDs(retried))
	require.Len(t, db.rules, 2)
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: replacementRows(db.owner), ExpectedState: initial.StateToken()})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	saved, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: replacementRows(db.owner), ExpectedState: current.StateToken()})
	require.NoError(t, err)
	require.Len(t, saved.Rules(), 1)
	require.Len(t, saved.Items(), 9)
	require.Equal(t, "600", saved.Summary().ScheduledAmount.String())
	require.Equal(t, "600", saved.Summary().AssignedAmount.String())
	require.Len(t, db.rules, 3)
	for _, item := range saved.Items() {
		if item.ScheduleStatus == types.BillingScheduleInvoiced {
			require.Equal(t, types.StatusArchived, item.Status)
		} else {
			require.Equal(t, "150", item.Amount.String())
		}
	}
	preview, err := core.Preview(ctx, PreviewRequest{Owner: db.owner.Owner, Rows: replacementRows(db.owner)})
	require.NoError(t, err)
	require.True(t, preview.Noop())
	require.True(t, preview.CanApply())
	require.Nil(t, preview.Plan())
	for _, item := range saved.Items() {
		if item.ScheduleStatus == types.BillingScheduleScheduled {
			claimMemory(db, item, types.InvoiceStatusDraft)
			break
		}
	}
	retried, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: replacementRows(db.owner), ExpectedState: saved.StateToken()})
	require.NoError(t, err)
	require.True(t, retried.Noop())
	require.Equal(t, "750", retried.Summary().AssignedAmount.String())
	require.Equal(t, "450", retried.Summary().ScheduledAmount.String())
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: annualRows(), ExpectedState: initial.StateToken()})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
}

func TestCoreVoidGapCorrectionAndRollback(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	for _, item := range initial.Items()[:5] {
		claimMemory(db, item, types.InvoiceStatusFinalized)
	}
	current, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	db.failCreate = true
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: replacementRows(db.owner), ExpectedState: current.StateToken()})
	require.ErrorContains(t, err, "injected")
	db.failCreate = false
	afterFailure, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, current.StateToken(), afterFailure.StateToken())
	require.Equal(t, sourceIDs(current), sourceIDs(afterFailure))
	invoice := db.bindings[initial.Items()[0].ID].InvoiceID
	conditional, err := core.Preview(ctx, PreviewRequest{Owner: db.owner.Owner, Rows: []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(db.owner.EndDate)}}, ReplaceInvoiceIDs: []string{invoice}})
	require.NoError(t, err)
	require.False(t, conditional.CanApply())
	require.Equal(t, "300", conditional.ConditionalReleasedPrincipal().String())
	require.Equal(t, "900", conditional.Plan().Budget().String())
	unchanged, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, current.StateToken(), unchanged.StateToken())
	_, err = core.Preview(ctx, PreviewRequest{Owner: db.owner.Owner, Rows: annualRows(), ReplaceInvoiceIDs: []string{"foreign-invoice"}})
	require.True(t, ierr.IsNotFound(err), "%+v", err)
	item := initial.Items()[0]
	binding := db.bindings[item.ID]
	binding.InvoiceStatus = types.InvoiceStatusVoided
	db.bindings[item.ID] = binding
	_, err = core.Get(ctx, db.owner.Owner)
	require.True(t, ierr.IsInternal(err), "%+v", err)
	_, err = (&memoryItems{db}).Cancel(ctx, item.ID, types.BillingScheduleInvoiceVoided)
	require.NoError(t, err)
	gapped, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.True(t, gapped.Summary().RequiresReplan)
	require.Equal(t, "300", gapped.Summary().UnallocatedAmount.String())
	require.Equal(t, "requires_replan", *gapped.Summary().DeferredReason)
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: annualRows(), ExpectedState: current.StateToken()})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	rows := []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(db.owner.EndDate)}}
	saved, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: rows, ExpectedState: gapped.StateToken()})
	require.NoError(t, err)
	require.False(t, saved.Summary().RequiresReplan)
	require.Equal(t, "300", saved.Summary().AssignedAmount.String())
	require.Equal(t, "900", saved.Summary().ScheduledAmount.String())
}

func TestCoreTriggerReplayAndArchivedConflict(t *testing.T) {
	ctx, core, db := newCore(t)
	db.owner.StartDate = core.(*service).now().Add(-time.Hour)
	db.owner.EndDate = db.owner.StartDate.AddDate(1, 0, 0)
	rows := []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: true}}
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: rows})
	require.NoError(t, err)
	itemID := initial.Items()[0].ID
	require.Nil(t, initial.Summary().NextBillOn)
	_, err = core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: itemID, ExpectedState: "stale"})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	triggered, err := core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: itemID, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.NotNil(t, triggered.Items()[0].BillOn)
	require.NotEqual(t, initial.StateToken(), triggered.StateToken())
	require.Equal(t, "1200", triggered.Summary().DueAmount.String())
	replayed, err := core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: itemID, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.True(t, replayed.Noop())
	require.Equal(t, *triggered.Items()[0].BillOn, *replayed.Items()[0].BillOn)
	_, err = core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: itemID, BillOn: ptr(db.owner.EndDate)})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	noop, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: rows, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.True(t, noop.Noop())
	require.Equal(t, *triggered.Items()[0].BillOn, *noop.Items()[0].BillOn)
	changed, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(db.owner.EndDate)}}, ExpectedState: noop.StateToken()})
	require.NoError(t, err)
	_, err = core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: itemID, ExpectedState: changed.StateToken()})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	_, err = core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: "missing"})
	require.True(t, ierr.IsNotFound(err), "%+v", err)
}

func TestCoreStateTokenAndDetachedViews(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	previousClock := core.(*service).now()
	core.(*service).now = func() time.Time { return previousClock.Add(time.Hour) }
	for _, row := range db.rules {
		row.CreatedAt = row.CreatedAt.Add(time.Hour)
		row.UpdatedAt = time.Now()
		row.UpdatedBy = "unrelated"
	}
	for _, row := range db.items {
		row.UpdatedAt = time.Now()
		row.UpdatedBy = "unrelated"
	}
	db.owner.AssignedAmount = decimal.NewFromInt(999)
	db.owner.RetainedItemCount = 500
	unchanged, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, initial.StateToken(), unchanged.StateToken())
	require.Equal(t, initial.Summary().AsOf.Add(time.Hour), unchanged.Summary().AsOf)
	require.Equal(t, "0", unchanged.Owner().AssignedAmount.String())
	mutations := []struct {
		name   string
		change func(*domain.OwnerSnapshot)
	}{
		{"lifecycle", func(owner *domain.OwnerSnapshot) { owner.LifecycleKey = "paused" }},
		{"alignment", func(owner *domain.OwnerSnapshot) { owner.AlignmentKey = "new-calendar" }},
		{"anchor", func(owner *domain.OwnerSnapshot) { owner.CalendarAnchor = owner.StartDate.Add(time.Hour) }},
		{"collection permission", func(owner *domain.OwnerSnapshot) { owner.CanCollect = false }},
		{"edit permission", func(owner *domain.OwnerSnapshot) { owner.CanEdit = false }},
		{"deferral", func(owner *domain.OwnerSnapshot) { owner.DeferredReason = "subscription_trialing" }},
	}
	baseline := db.owner
	for _, tt := range mutations {
		t.Run(tt.name, func(t *testing.T) {
			db.owner = baseline
			tt.change(&db.owner)
			changed, err := core.Get(ctx, db.owner.Owner)
			require.NoError(t, err)
			require.NotEqual(t, initial.StateToken(), changed.StateToken())
		})
	}
	db.owner = baseline
	rules := initial.Rules()
	rules[0].Description = "external mutation"
	rules[0].Metadata["external"] = "mutation"
	items := initial.Items()
	items[0].Amount = decimal.NewFromInt(1)
	*items[0].BillOn = items[0].BillOn.Add(time.Hour)
	summary := initial.Summary()
	summary.ScheduledAmount = decimal.NewFromInt(1)
	owner := initial.Owner()
	owner.OriginalTotal = decimal.NewFromInt(1)
	final, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, unchanged.StateToken(), final.StateToken())
	require.Equal(t, "1200", initial.Summary().ScheduledAmount.String())
}

func TestCoreIntegrityChecks(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*memoryExecution, *domain.ScheduleItem)
	}{
		{"missing invoice", func(db *memoryExecution, item *domain.ScheduleItem) {
			claimMemory(db, item, types.InvoiceStatusDraft)
			db.missingBinding = true
		}},
		{"foreign line", func(db *memoryExecution, item *domain.ScheduleItem) {
			claimMemory(db, item, types.InvoiceStatusDraft)
			db.wrongBinding = true
		}},
		{"voided invoice", func(db *memoryExecution, item *domain.ScheduleItem) { claimMemory(db, item, types.InvoiceStatusVoided) }},
		{"skipped invoice", func(db *memoryExecution, item *domain.ScheduleItem) {
			claimMemory(db, item, types.InvoiceStatusSkipped)
		}},
		{"foreign source scope", func(db *memoryExecution, item *domain.ScheduleItem) { db.items[item.ID].EnvironmentID = "foreign" }},
		{"service period outside owner", func(db *memoryExecution, item *domain.ScheduleItem) {
			db.items[item.ID].ServicePeriodEnd = db.owner.EndDate.Add(time.Hour)
		}},
		{"invalid amount", func(db *memoryExecution, item *domain.ScheduleItem) { db.items[item.ID].Amount = decimal.Zero }},
		{"missing current rule", func(db *memoryExecution, item *domain.ScheduleItem) {
			db.rules[*item.BillingTermID].Status = types.StatusArchived
		}},
		{"partial binding", func(db *memoryExecution, item *domain.ScheduleItem) { db.items[item.ID].InvoiceID = ptr("invoice") }},
		{"overallocated target", func(db *memoryExecution, _ *domain.ScheduleItem) {
			db.owner.CollectionTarget = decimal.NewFromInt(1000)
		}},
		{"bad calendar range", func(db *memoryExecution, _ *domain.ScheduleItem) { db.owner.EndDate = db.owner.StartDate }},
		{"fractional owner total", func(db *memoryExecution, _ *domain.ScheduleItem) {
			db.owner.OriginalTotal = decimal.RequireFromString("1200.001")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, core, db := newCore(t)
			initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
			require.NoError(t, err)
			tt.mutate(db, initial.Items()[0])
			_, err = core.Get(ctx, db.owner.Owner)
			require.True(t, ierr.IsInternal(err), "%+v", err)
		})
	}
}

func TestCoreBoundedRelevantHistory(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	for index := 0; index < 1000; index++ {
		row := domain.NewScheduleItemBuilder(initial.Items()[0]).WithID(types.GenerateUUID()).WithStatus(types.StatusArchived).WithScheduleStatus(types.BillingScheduleCanceled).WithCancellationReason(ptr(types.BillingScheduleTermsAmended)).Build()
		row.CreatedAt = db.stamp()
		db.items[row.ID] = row
	}
	current, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Len(t, current.Items(), 13)
	require.Equal(t, initial.StateToken(), current.StateToken())
	for len(db.items) < MaxItems+1001 {
		row := domain.NewScheduleItemBuilder(initial.Items()[0]).WithID(types.GenerateUUID()).Build()
		row.CreatedAt = db.stamp()
		db.items[row.ID] = row
	}
	_, err = core.Get(ctx, db.owner.Owner)
	require.True(t, ierr.IsInternal(err), "%+v", err)
}

func TestCoreDuplicateRulesFullyAssignedNoop(t *testing.T) {
	ctx, core, db := newCore(t)
	row := RuleInput{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: ptr(decimal.NewFromInt(600))}
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: []RuleInput{row, row}})
	require.NoError(t, err)
	require.Len(t, initial.Items(), 2)
	for _, item := range initial.Items() {
		claimMemory(db, item, types.InvoiceStatusFinalized)
	}
	current, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: []RuleInput{row, row}, ExpectedState: initial.StateToken()})
	require.NoError(t, err)
	require.True(t, current.Noop())
	require.Equal(t, "1200", current.Summary().AssignedAmount.String())
	require.Equal(t, "0", current.Summary().ScheduledAmount.String())
	require.Equal(t, sourceIDs(initial), sourceIDs(current))
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermFixed, Value: ptr(decimal.NewFromInt(1200))}}, ExpectedState: initial.StateToken()})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
}

func TestCoreConcurrentEditsAndNestedTransaction(t *testing.T) {
	ctx, core, db := newCore(t)
	var initial State
	err := db.WithTx(ctx, func(tx context.Context) error {
		var err error
		initial, err = core.Initialize(tx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
		return err
	})
	require.NoError(t, err)
	rows := [][]RuleInput{
		{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(db.owner.EndDate)}},
		{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(db.owner.StartDate)}},
	}
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, proposal := range rows {
		workers.Add(1)
		go func(proposal []RuleInput) {
			defer workers.Done()
			_, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: proposal, ExpectedState: initial.StateToken()})
			results <- err
		}(proposal)
	}
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		if err == nil {
			successes++
		} else if ierr.IsVersionConflict(err) {
			conflicts++
		} else {
			t.Fatalf("unexpected edit result: %+v", err)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
}

func TestCoreUnavailableAndResolverValidation(t *testing.T) {
	ctx, _, db := newCore(t)
	var missing *service
	_, err := missing.Get(ctx, db.owner.Owner)
	require.Error(t, err)
	_, err = missing.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner})
	require.Error(t, err)
	_, err = missing.Preview(ctx, PreviewRequest{Owner: db.owner.Owner})
	require.Error(t, err)
	_, err = missing.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner})
	require.Error(t, err)
	_, err = missing.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner})
	require.Error(t, err)
	require.Nil(t, NewService(nil, nil, nil, nil))
	var adapter *memoryOwner
	_, err = NewOwnerResolver(adapter)
	require.Error(t, err)
	_, err = NewOwnerResolver(&memoryOwner{db}, &memoryOwner{db})
	require.Error(t, err)
	resolver, err := NewOwnerResolver(&memoryOwner{db})
	require.NoError(t, err)
	_, err = resolver.Snapshot(context.Background(), db.owner.Owner)
	require.True(t, ierr.IsValidation(err), "%+v", err)
	_, err = resolver.Snapshot(ctx, domain.OwnerRef{Type: "future", ID: db.owner.Owner.ID})
	require.True(t, ierr.IsValidation(err), "%+v", err)
	_, err = resolver.Snapshot(ctx, domain.OwnerRef{Type: db.owner.Owner.Type, ID: "foreign"})
	require.True(t, ierr.IsNotFound(err), "%+v", err)
}

func TestCoreTimestampPrecisionRetry(t *testing.T) {
	for _, manual := range []bool{false, true} {
		t.Run(map[bool]string{false: "dated", true: "manual"}[manual], func(t *testing.T) {
			ctx, core, db := newCore(t)
			requested := db.owner.StartDate.Add(time.Hour + 123456789*time.Nanosecond)
			row := RuleInput{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, OnTrigger: manual}
			if !manual {
				row.BillOn = &requested
			}
			initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: []RuleInput{row}})
			require.NoError(t, err)
			if manual {
				initial, err = core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: initial.Items()[0].ID, ExpectedState: initial.StateToken(), BillOn: &requested})
				require.NoError(t, err)
				replayed, err := core.Trigger(ctx, TriggerRequest{Owner: db.owner.Owner, ItemID: initial.Items()[0].ID, BillOn: &requested})
				require.NoError(t, err)
				require.True(t, replayed.Noop())
			}
			require.Equal(t, billingDate(requested), *initial.Items()[0].BillOn)
			retried, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: []RuleInput{row}, ExpectedState: "earlier-state"})
			require.NoError(t, err)
			require.True(t, retried.Noop())
			require.Equal(t, sourceIDs(initial), sourceIDs(retried))
		})
	}
}

func TestCoreFullReleaseAndDeferralSummary(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	for _, item := range initial.Items() {
		claimMemory(db, item, types.InvoiceStatusFinalized)
		_, err := (&memoryItems{db}).Cancel(ctx, item.ID, types.BillingScheduleInvoiceVoided)
		require.NoError(t, err)
	}
	gap, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	require.Equal(t, "1200", gap.Summary().UnallocatedAmount.String())
	require.Len(t, gap.Items(), 0)
	rows := annualRows()
	rows[0].BillOn = ptr(db.owner.StartDate)
	rows[1].StartDate = ptr(db.owner.StartDate)
	_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: rows, ExpectedState: initial.StateToken()})
	require.True(t, ierr.IsVersionConflict(err), "%+v", err)
	saved, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: rows, ExpectedState: gap.StateToken()})
	require.NoError(t, err)
	require.False(t, saved.Noop())
	require.Equal(t, "1200", saved.Summary().ScheduledAmount.String())
	require.False(t, saved.Summary().RequiresReplan)
	for _, reason := range []string{"subscription_paused", "subscription_trialing", "subscription_pending", "delivery_disabled"} {
		t.Run(reason, func(t *testing.T) {
			db.owner.CanCollect = false
			db.owner.DeferredReason = reason
			view, err := core.Get(ctx, db.owner.Owner)
			require.NoError(t, err)
			require.Equal(t, reason, *view.Summary().DeferredReason)
			require.Equal(t, "1200", view.Summary().ScheduledAmount.String())
		})
	}
}

func TestPlannerOwnerCadenceDefaults(t *testing.T) {
	req := testRequest(t)
	req.Owner.DefaultInterval = types.BILLING_PERIOD_MONTHLY
	req.Owner.DefaultIntervalCount = 2
	row := recurringRule()
	row.Interval = nil
	row.IntervalCount = nil
	req.Rows = []RuleInput{row}
	plan, err := NewPlanner().Plan(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, types.BILLING_PERIOD_MONTHLY, *plan.Rules()[0].Interval)
	require.Equal(t, 2, *plan.Rules()[0].IntervalCount)
	require.Len(t, plan.Items(), 6)
}

func TestCorePendingContextChangeRequiresReviewedReplan(t *testing.T) {
	tests := []struct {
		name   string
		change func(*domain.OwnerSnapshot)
	}{
		{"recipient", func(owner *domain.OwnerSnapshot) { owner.CustomerID = "new-invoicing-customer" }},
		{"subscription", func(owner *domain.OwnerSnapshot) { owner.SubscriptionID = ptr("new-subscription") }},
		{"delivery", func(owner *domain.OwnerSnapshot) { owner.Delivery = types.BillingTermSeparateInvoice }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, core, db := newCore(t)
			initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
			require.NoError(t, err)
			assigned := initial.Items()[0]
			claimMemory(db, assigned, types.InvoiceStatusFinalized)
			before, err := core.Get(ctx, db.owner.Owner)
			require.NoError(t, err)
			tt.change(&db.owner)
			drifted, err := core.Get(ctx, db.owner.Owner)
			require.NoError(t, err)
			require.True(t, drifted.Summary().RequiresReplan)
			require.Equal(t, "0", drifted.Summary().UnallocatedAmount.String())
			require.Equal(t, "requires_replan", *drifted.Summary().DeferredReason)
			require.Equal(t, "300", drifted.Summary().AssignedAmount.String())
			require.Equal(t, "900", drifted.Summary().ScheduledAmount.String())
			_, err = core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: annualRows(), ExpectedState: before.StateToken()})
			require.True(t, ierr.IsVersionConflict(err), "%+v", err)
			db.owner.CanCollect = false
			db.owner.DeferredReason = "subscription_paused"
			paused, err := core.Get(ctx, db.owner.Owner)
			require.NoError(t, err)
			require.Equal(t, "subscription_paused", *paused.Summary().DeferredReason)
			db.owner.CanCollect = true
			db.owner.DeferredReason = ""
			drifted, err = core.Get(ctx, db.owner.Owner)
			require.NoError(t, err)
			rows := []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(db.owner.EndDate)}}
			saved, err := core.Replace(ctx, ReplaceRequest{Owner: db.owner.Owner, Rows: rows, ExpectedState: drifted.StateToken()})
			require.NoError(t, err)
			require.False(t, saved.Summary().RequiresReplan)
			require.Equal(t, "300", saved.Summary().AssignedAmount.String())
			require.Equal(t, "900", saved.Summary().ScheduledAmount.String())
			for _, item := range saved.Items() {
				if item.ScheduleStatus == types.BillingScheduleInvoiced {
					require.Equal(t, assigned.ID, item.ID)
					require.Equal(t, assigned.CustomerID, item.CustomerID)
					require.Equal(t, assigned.SubscriptionID, item.SubscriptionID)
					require.Equal(t, assigned.Delivery, item.Delivery)
					require.Equal(t, types.StatusArchived, item.Status)
				} else {
					require.Equal(t, db.owner.CustomerID, item.CustomerID)
					require.Equal(t, db.owner.SubscriptionID, item.SubscriptionID)
					require.Equal(t, db.owner.Delivery, item.Delivery)
				}
			}
		})
	}
}

func TestCorrectionPreviewReleasesEverySourceForSelectedInvoice(t *testing.T) {
	ctx, core, db := newCore(t)
	initial, err := core.Initialize(ctx, InitializeRequest{Owner: db.owner.Owner, Rows: annualRows()})
	require.NoError(t, err)
	invoiceID := "shared-invoice"
	for _, item := range initial.Items()[:5] {
		claimMemory(db, item, types.InvoiceStatusFinalized)
		db.items[item.ID] = domain.NewScheduleItemBuilder(db.items[item.ID]).WithInvoiceID(&invoiceID).Build()
		binding := db.bindings[item.ID]
		binding.InvoiceID = invoiceID
		db.bindings[item.ID] = binding
	}
	current, err := core.Get(ctx, db.owner.Owner)
	require.NoError(t, err)
	rows := []RuleInput{{Kind: types.BillingTermOnce, ValueType: types.BillingTermRemainder, BillOn: ptr(db.owner.EndDate)}}
	proposed, err := core.Preview(ctx, PreviewRequest{Owner: db.owner.Owner, Rows: rows, ReplaceInvoiceIDs: []string{invoiceID}})
	require.NoError(t, err)
	require.False(t, proposed.CanApply())
	require.Equal(t, "600", proposed.ConditionalReleasedPrincipal().String())
	require.Equal(t, "1200", proposed.Plan().Budget().String())
	require.Equal(t, current.StateToken(), proposed.Current().StateToken())
	require.Equal(t, "600", proposed.Current().Summary().AssignedAmount.String())
	for _, ids := range [][]string{{" "}, {strings.Repeat("x", 51)}, {invoiceID, invoiceID}} {
		_, err := core.Preview(ctx, PreviewRequest{Owner: db.owner.Owner, Rows: rows, ReplaceInvoiceIDs: ids})
		require.True(t, ierr.IsValidation(err), "%+v", err)
	}
}
