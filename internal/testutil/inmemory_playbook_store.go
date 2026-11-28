package testutil

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"

	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// InMemoryPlaybookStore implements collections.PlaybookRepository and collections.PlaybookRunRepository over the
// in-memory invoice and customer stores, with the semantics of the SQL in internal/repository/ent/collection_playbook.go.
type InMemoryPlaybookStore struct {
	invoices  *InMemoryInvoiceStore
	customers *InMemoryCustomerStore

	mu        sync.Mutex
	playbooks map[string]*collections.Playbook
	runs      map[string]*collections.PlaybookRun
	steps     map[string]*collections.PlaybookStepRun
	tasks     map[string]*collections.CollectionTask
}

func NewInMemoryPlaybookStore(invoices *InMemoryInvoiceStore, customers *InMemoryCustomerStore) *InMemoryPlaybookStore {
	return &InMemoryPlaybookStore{
		invoices: invoices, customers: customers,
		playbooks: map[string]*collections.Playbook{}, runs: map[string]*collections.PlaybookRun{},
		steps: map[string]*collections.PlaybookStepRun{}, tasks: map[string]*collections.CollectionTask{},
	}
}

func scoped(ctx context.Context, tenantID, envID string) bool {
	return tenantID == types.GetTenantID(ctx) && envID == types.GetEnvironmentID(ctx)
}

func clonePlaybook(p *collections.Playbook) *collections.Playbook {
	cp := *p
	cp.Steps = lo.Map(p.Steps, func(s types.PlaybookStep, _ int) types.PlaybookStep {
		if s.Assignee != nil {
			a := *s.Assignee
			s.Assignee = &a
		}
		return s
	})
	cp.Stops = append([]types.PlaybookStopCondition(nil), p.Stops...)
	if p.Audience.Amount != nil {
		a := *p.Audience.Amount
		cp.Audience.Amount = &a
	}
	if p.ActivatedAt != nil {
		cp.ActivatedAt = lo.ToPtr(*p.ActivatedAt)
	}
	return &cp
}

func (s *InMemoryPlaybookStore) Create(ctx context.Context, p *collections.Playbook) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	p.TenantID, p.EnvironmentID, p.BaseModel.Status = types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.StatusPublished
	s.playbooks[p.ID] = clonePlaybook(p)
	return nil
}

func (s *InMemoryPlaybookStore) livePlaybook(ctx context.Context, id string) (*collections.Playbook, bool) {
	p, ok := s.playbooks[id]
	if !ok || p.BaseModel.Status != types.StatusPublished || !scoped(ctx, p.TenantID, p.EnvironmentID) {
		return nil, false
	}
	return p, true
}

func (s *InMemoryPlaybookStore) Get(ctx context.Context, id string) (*collections.Playbook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.livePlaybook(ctx, id)
	if !ok {
		return nil, ierr.NewErrorf("playbook %s not found", id).Mark(ierr.ErrNotFound)
	}
	return clonePlaybook(p), nil
}

func (s *InMemoryPlaybookStore) List(ctx context.Context, filter collections.PlaybookFilter) ([]*collections.Playbook, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*collections.Playbook
	for id := range s.playbooks {
		p, ok := s.livePlaybook(ctx, id)
		if !ok || (len(filter.States) > 0 && !lo.Contains(filter.States, p.State)) {
			continue
		}
		out = append(out, clonePlaybook(p))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Priority != out[j].Priority {
			return out[i].Priority < out[j].Priority
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *InMemoryPlaybookStore) Update(ctx context.Context, p *collections.Playbook, previousVersion int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.livePlaybook(ctx, p.ID)
	if !ok || cur.Version != previousVersion {
		return ierr.NewError("playbook changed").Mark(ierr.ErrVersionConflict)
	}
	cp := clonePlaybook(p)
	cp.TenantID, cp.EnvironmentID, cp.BaseModel.Status, cp.CreatedAt = cur.TenantID, cur.EnvironmentID, cur.BaseModel.Status, cur.CreatedAt
	cp.Version, cp.UpdatedAt, cp.UpdatedBy = previousVersion+1, clock.Now(), types.GetUserID(ctx)
	s.playbooks[p.ID] = cp
	*p = *clonePlaybook(cp)
	return nil
}

func (s *InMemoryPlaybookStore) SetPriorities(ctx context.Context, priorities map[string]int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range priorities {
		if _, ok := s.livePlaybook(ctx, id); !ok {
			return ierr.NewErrorf("playbook %s not found", id).Mark(ierr.ErrNotFound)
		}
	}
	for id, priority := range priorities {
		s.playbooks[id].Priority = priority
	}
	return nil
}

func (s *InMemoryPlaybookStore) Delete(ctx context.Context, p *collections.Playbook) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.livePlaybook(ctx, p.ID)
	if !ok {
		return ierr.NewErrorf("playbook %s not found", p.ID).Mark(ierr.ErrNotFound)
	}
	cur.BaseModel.Status = types.StatusDeleted
	return nil
}

func (s *InMemoryPlaybookStore) MaxPriority(ctx context.Context) (int, error) {
	list, err := s.List(ctx, collections.PlaybookFilter{})
	if err != nil || len(list) == 0 {
		return 0, err
	}
	return list[len(list)-1].Priority, nil
}

func (s *InMemoryPlaybookStore) ListLiveEnvironments(context.Context) ([]collections.PlaybookEnvironment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[collections.PlaybookEnvironment]bool{}
	for _, p := range s.playbooks {
		if p.BaseModel.Status == types.StatusPublished && p.State == types.PlaybookStateLive {
			seen[collections.PlaybookEnvironment{TenantID: p.TenantID, EnvironmentID: p.EnvironmentID}] = true
		}
	}
	out := lo.Keys(seen)
	sort.Slice(out, func(i, j int) bool {
		return out[i].TenantID+"/"+out[i].EnvironmentID < out[j].TenantID+"/"+out[j].EnvironmentID
	})
	return out, nil
}

func (s *InMemoryPlaybookStore) Stats(ctx context.Context, playbookIDs []string, now time.Time) (map[string]collections.PlaybookStats, error) {
	out := map[string]collections.PlaybookStats{}
	since := now.Add(-30 * 24 * time.Hour)
	s.mu.Lock()
	runs := lo.Filter(lo.Values(s.runs), func(r *collections.PlaybookRun, _ int) bool {
		return scoped(ctx, r.TenantID, r.EnvironmentID) && lo.Contains(playbookIDs, r.PlaybookID)
	})
	steps := lo.Values(s.steps)
	s.mu.Unlock()

	type key struct{ playbook, currency string }
	recovered := map[key]decimal.Decimal{}
	for _, r := range runs {
		st := out[r.PlaybookID]
		if r.IsLive() {
			st.InProgress++
		}
		var sent []*collections.PlaybookStepRun
		for _, sr := range steps {
			if sr.RunID == r.ID && sr.Kind == types.PlaybookStepEmail && sr.State == types.PlaybookStepRunDone && sr.ExecutedAt != nil && !sr.ExecutedAt.Before(since) {
				sent = append(sent, sr)
			}
		}
		st.RemindersSent30d += len(sent)
		if len(sent) > 0 {
			st.RemindedInvoices30d++
			inv, err := s.invoices.Get(ctx, r.InvoiceID)
			if err == nil && inv.PaidAt != nil && (inv.PaymentStatus == types.PaymentStatusSucceeded || inv.PaymentStatus == types.PaymentStatusOverpaid) {
				if lo.SomeBy(sent, func(sr *collections.PlaybookStepRun) bool {
					return !inv.PaidAt.Before(*sr.ExecutedAt) && !inv.PaidAt.After(sr.ExecutedAt.Add(7*24*time.Hour))
				}) {
					st.PaidWithin7d30d++
					k := key{r.PlaybookID, strings.ToLower(inv.Currency)}
					recovered[k] = recovered[k].Add(inv.AmountPaid)
				}
			}
		}
		out[r.PlaybookID] = st
	}
	for k, amount := range recovered {
		st := out[k.playbook]
		st.Recovered30d = append(st.Recovered30d, collections.CurrencyTotal{Currency: k.currency, Amount: amount})
		out[k.playbook] = st
	}
	return out, nil
}

// --- runs ---

func (s *InMemoryPlaybookStore) openInvoices(ctx context.Context) ([]*invoice.Invoice, error) {
	filter := types.NewNoLimitQueryFilter()
	filter.Status = lo.ToPtr(types.StatusPublished)
	list, err := s.invoices.List(ctx, &types.InvoiceFilter{QueryFilter: filter, InvoiceStatus: []types.InvoiceStatus{types.InvoiceStatusFinalized}})
	if err != nil {
		return nil, err
	}
	return lo.Filter(list, func(inv *invoice.Invoice, _ int) bool {
		return inv.PaymentStatus != types.PaymentStatusSucceeded && inv.PaymentStatus != types.PaymentStatusOverpaid &&
			inv.AmountRemaining.IsPositive() && inv.DueDate != nil
	}), nil
}

func (s *InMemoryPlaybookStore) ListCandidates(ctx context.Context, p *collections.Playbook, now time.Time, limit int) ([]collections.PlaybookCandidate, error) {
	invoices, err := s.openInvoices(ctx)
	if err != nil {
		return nil, err
	}
	from, before := collections.CandidateDueWindow(p, now)
	overdueBefore := types.UTCDay(now).AddDate(0, 0, -p.Audience.Days)

	s.mu.Lock()
	blocked := map[string]bool{}
	for _, r := range s.runs {
		if scoped(ctx, r.TenantID, r.EnvironmentID) && r.BaseModel.Status == types.StatusPublished &&
			!(r.State == types.PlaybookRunStopped && r.StopReason == types.PlaybookStopReasonPlaybookDeleted) {
			blocked[r.InvoiceID] = true
		}
	}
	s.mu.Unlock()

	var out []collections.PlaybookCandidate
	for _, inv := range invoices {
		due := *inv.DueDate
		if blocked[inv.ID] || due.Before(from) || !due.Before(before) {
			continue
		}
		c, _ := s.customers.Get(ctx, inv.CustomerID)
		a := p.Audience
		match := false
		switch a.Kind {
		case types.PlaybookAudienceAll:
			match = true
		case types.PlaybookAudienceSegment:
			if c != nil {
				match = strings.EqualFold(c.Metadata[a.Key], a.Value)
			}
		case types.PlaybookAudienceAmountOver:
			match = strings.EqualFold(inv.Currency, a.Currency) && a.Amount != nil && inv.AmountRemaining.GreaterThan(*a.Amount)
		case types.PlaybookAudienceDaysOverdueOver:
			match = due.Before(overdueBefore)
		}
		if !match {
			continue
		}
		name := ""
		if c != nil {
			name = c.Name
		}
		out = append(out, collections.PlaybookCandidate{
			InvoiceID: inv.ID, InvoiceNumber: lo.FromPtr(inv.InvoiceNumber), CustomerID: inv.CustomerID, CustomerName: name,
			Currency: strings.ToLower(inv.Currency), AmountRemaining: inv.AmountRemaining, DueDate: due,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DueDate.Equal(out[j].DueDate) {
			return out[i].DueDate.Before(out[j].DueDate)
		}
		return out[i].InvoiceID < out[j].InvoiceID
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *InMemoryPlaybookStore) CreateRun(ctx context.Context, run *collections.PlaybookRun, steps []*collections.PlaybookStepRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.runs {
		if scoped(ctx, r.TenantID, r.EnvironmentID) && r.InvoiceID == run.InvoiceID && r.IsLive() {
			return ierr.NewError("invoice already in a playbook").Mark(ierr.ErrAlreadyExists)
		}
	}
	run.TenantID, run.EnvironmentID, run.BaseModel.Status = types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.StatusPublished
	cp := *run
	s.runs[run.ID] = &cp
	return s.createStepRunsLocked(ctx, steps)
}

func (s *InMemoryPlaybookStore) GetRun(ctx context.Context, id string) (*collections.PlaybookRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok || !scoped(ctx, r.TenantID, r.EnvironmentID) {
		return nil, ierr.NewErrorf("playbook run %s not found", id).Mark(ierr.ErrNotFound)
	}
	cp := *r
	return &cp, nil
}

func (s *InMemoryPlaybookStore) UpdateRun(ctx context.Context, run *collections.PlaybookRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.runs[run.ID]
	if !ok || !scoped(ctx, cur.TenantID, cur.EnvironmentID) || !cur.UpdatedAt.Equal(run.UpdatedAt) {
		return ierr.NewError("playbook run changed").Mark(ierr.ErrVersionConflict)
	}
	cp := *run
	cp.UpdatedAt = clock.Now()
	if !cp.UpdatedAt.After(cur.UpdatedAt) {
		cp.UpdatedAt = cur.UpdatedAt.Add(time.Microsecond)
	}
	s.runs[run.ID] = &cp
	*run = cp
	return nil
}

func (s *InMemoryPlaybookStore) ListLiveRuns(ctx context.Context, playbookID, customerID string) ([]*collections.PlaybookRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*collections.PlaybookRun
	for _, r := range s.runs {
		if !scoped(ctx, r.TenantID, r.EnvironmentID) || !r.IsLive() ||
			(playbookID != "" && r.PlaybookID != playbookID) || (customerID != "" && r.CustomerID != customerID) {
			continue
		}
		cp := *r
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *InMemoryPlaybookStore) ListRunViews(ctx context.Context, playbookID string, states []types.PlaybookRunState, offset, limit int) ([]collections.PlaybookRunView, int, error) {
	s.mu.Lock()
	var runs []collections.PlaybookRun
	for _, r := range s.runs {
		if scoped(ctx, r.TenantID, r.EnvironmentID) && r.PlaybookID == playbookID && lo.Contains(states, r.State) {
			runs = append(runs, *r)
		}
	}
	s.mu.Unlock()
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].InvoiceDueDate.Equal(runs[j].InvoiceDueDate) {
			return runs[i].InvoiceDueDate.Before(runs[j].InvoiceDueDate)
		}
		return runs[i].ID < runs[j].ID
	})
	total := len(runs)
	runs = lo.Slice(runs, offset, offset+limit)
	steps, _ := s.ListStepRuns(ctx, lo.Map(runs, func(r collections.PlaybookRun, _ int) string { return r.ID }))
	next := collections.NextStepByRun(steps)
	out := make([]collections.PlaybookRunView, 0, len(runs))
	for _, r := range runs {
		v := collections.PlaybookRunView{Run: r, NextStep: next[r.ID]}
		if inv, err := s.invoices.Get(ctx, r.InvoiceID); err == nil {
			v.InvoiceNumber, v.Currency, v.AmountRemaining = lo.FromPtr(inv.InvoiceNumber), strings.ToLower(inv.Currency), inv.AmountRemaining
		}
		if c, err := s.customers.Get(ctx, r.CustomerID); err == nil {
			v.CustomerName = c.Name
		}
		out = append(out, v)
	}
	return out, total, nil
}

func (s *InMemoryPlaybookStore) StopFinishedRuns(ctx context.Context, now time.Time) (int, error) {
	s.mu.Lock()
	live := lo.Filter(lo.Values(s.runs), func(r *collections.PlaybookRun, _ int) bool {
		return scoped(ctx, r.TenantID, r.EnvironmentID) && r.IsLive()
	})
	s.mu.Unlock()
	stopped := 0
	for _, r := range live {
		inv, err := s.invoices.Get(ctx, r.InvoiceID)
		var reason types.PlaybookStopReason
		switch {
		case err != nil || inv.Status != types.StatusPublished || inv.InvoiceStatus != types.InvoiceStatusFinalized:
			reason = types.PlaybookStopReasonVoided
		case inv.PaymentStatus == types.PaymentStatusSucceeded || inv.PaymentStatus == types.PaymentStatusOverpaid || !inv.AmountRemaining.IsPositive():
			reason = types.PlaybookStopReasonPaid
		default:
			continue
		}
		s.mu.Lock()
		cur := s.runs[r.ID]
		cur.State, cur.StopReason, cur.EndedAt, cur.NextStepAt, cur.UpdatedAt = types.PlaybookRunStopped, reason, lo.ToPtr(now), nil, now
		for _, sr := range s.steps {
			if sr.RunID == r.ID && sr.State == types.PlaybookStepRunPending {
				sr.State, sr.SkipReason = types.PlaybookStepRunSkipped, types.PlaybookSkipRunStopped
			}
		}
		s.mu.Unlock()
		stopped++
	}
	return stopped, nil
}

func (s *InMemoryPlaybookStore) ListStepRuns(ctx context.Context, runIDs []string) ([]*collections.PlaybookStepRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*collections.PlaybookStepRun
	for _, sr := range s.steps {
		if scoped(ctx, sr.TenantID, sr.EnvironmentID) && lo.Contains(runIDs, sr.RunID) {
			cp := *sr
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].DueAt.Equal(out[j].DueAt) {
			return out[i].DueAt.Before(out[j].DueAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *InMemoryPlaybookStore) ListDueRunIDs(ctx context.Context, now time.Time, limit int) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := map[string]bool{}
	for _, sr := range s.steps {
		if !scoped(ctx, sr.TenantID, sr.EnvironmentID) || sr.State != types.PlaybookStepRunPending || sr.DueAt.After(now) {
			continue
		}
		r := s.runs[sr.RunID]
		if r == nil || r.State != types.PlaybookRunActive {
			continue
		}
		if p, ok := s.livePlaybook(ctx, r.PlaybookID); !ok || p.State != types.PlaybookStateLive {
			continue
		}
		ids[r.ID] = true
	}
	out := lo.Keys(ids)
	sort.Strings(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (s *InMemoryPlaybookStore) CreateStepRuns(ctx context.Context, steps []*collections.PlaybookStepRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createStepRunsLocked(ctx, steps)
}

func (s *InMemoryPlaybookStore) createStepRunsLocked(ctx context.Context, steps []*collections.PlaybookStepRun) error {
	for _, n := range steps {
		for _, sr := range s.steps {
			if sr.RunID == n.RunID && sr.StepID == n.StepID {
				return ierr.NewError("step already scheduled").Mark(ierr.ErrAlreadyExists)
			}
		}
	}
	for _, n := range steps {
		n.TenantID, n.EnvironmentID = types.GetTenantID(ctx), types.GetEnvironmentID(ctx)
		cp := *n
		s.steps[n.ID] = &cp
	}
	return nil
}

func (s *InMemoryPlaybookStore) UpdateStepRun(ctx context.Context, step *collections.PlaybookStepRun, previousState types.PlaybookStepRunState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.steps[step.ID]
	if !ok || !scoped(ctx, cur.TenantID, cur.EnvironmentID) || cur.State != previousState {
		return ierr.NewError("playbook step changed").Mark(ierr.ErrVersionConflict)
	}
	cp := *step
	cp.UpdatedAt = clock.Now()
	s.steps[step.ID] = &cp
	*step = cp
	return nil
}

func (s *InMemoryPlaybookStore) LastCustomerEmailAt(ctx context.Context, customerID string) (*time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var last *time.Time
	for _, sr := range s.steps {
		r := s.runs[sr.RunID]
		if r == nil || !scoped(ctx, sr.TenantID, sr.EnvironmentID) || r.CustomerID != customerID ||
			sr.Kind != types.PlaybookStepEmail || sr.State != types.PlaybookStepRunDone || sr.ExecutedAt == nil {
			continue
		}
		if last == nil || sr.ExecutedAt.After(*last) {
			last = lo.ToPtr(*sr.ExecutedAt)
		}
	}
	return last, nil
}

func (s *InMemoryPlaybookStore) CreateTask(ctx context.Context, t *collections.CollectionTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cur := range s.tasks {
		if t.RunID != "" && cur.RunID == t.RunID && cur.StepID == t.StepID {
			return ierr.NewError("task already created").Mark(ierr.ErrAlreadyExists)
		}
	}
	t.TenantID, t.EnvironmentID, t.BaseModel.Status = types.GetTenantID(ctx), types.GetEnvironmentID(ctx), types.StatusPublished
	cp := *t
	s.tasks[t.ID] = &cp
	return nil
}

func (s *InMemoryPlaybookStore) GetTask(ctx context.Context, id string) (*collections.CollectionTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok || !scoped(ctx, t.TenantID, t.EnvironmentID) {
		return nil, ierr.NewErrorf("task %s not found", id).Mark(ierr.ErrNotFound)
	}
	cp := *t
	return &cp, nil
}

func (s *InMemoryPlaybookStore) UpdateTask(ctx context.Context, t *collections.CollectionTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, ok := s.tasks[t.ID]
	if !ok || !scoped(ctx, cur.TenantID, cur.EnvironmentID) || !cur.UpdatedAt.Equal(t.UpdatedAt) {
		return ierr.NewError("task changed").Mark(ierr.ErrVersionConflict)
	}
	cp := *t
	cp.UpdatedAt = clock.Now()
	if !cp.UpdatedAt.After(cur.UpdatedAt) {
		cp.UpdatedAt = cur.UpdatedAt.Add(time.Microsecond)
	}
	s.tasks[t.ID] = &cp
	*t = cp
	return nil
}

func (s *InMemoryPlaybookStore) ListTasks(ctx context.Context, f collections.TaskFilter, offset, limit int) ([]*collections.CollectionTask, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*collections.CollectionTask
	for _, t := range s.tasks {
		if !scoped(ctx, t.TenantID, t.EnvironmentID) ||
			(f.InvoiceID != "" && t.InvoiceID != f.InvoiceID) || (f.CustomerID != "" && t.CustomerID != f.CustomerID) ||
			(f.AssigneeUserID != "" && t.AssigneeUserID != f.AssigneeUserID) || (len(f.States) > 0 && !lo.Contains(f.States, t.State)) {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt) || (out[i].CreatedAt.Equal(out[j].CreatedAt) && out[i].ID > out[j].ID)
	})
	total := len(out)
	return lo.Slice(out, offset, offset+limit), total, nil
}

// StepRuns returns every step run of the environment in ctx, for assertions.
func (s *InMemoryPlaybookStore) StepRuns(ctx context.Context) []*collections.PlaybookStepRun {
	s.mu.Lock()
	ids := lo.Uniq(lo.FilterMap(lo.Values(s.runs), func(r *collections.PlaybookRun, _ int) (string, bool) {
		return r.ID, scoped(ctx, r.TenantID, r.EnvironmentID)
	}))
	s.mu.Unlock()
	out, _ := s.ListStepRuns(ctx, ids)
	return out
}

// Runs returns every run of the environment in ctx, for assertions.
func (s *InMemoryPlaybookStore) Runs(ctx context.Context) []*collections.PlaybookRun {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []*collections.PlaybookRun
	for _, r := range s.runs {
		if scoped(ctx, r.TenantID, r.EnvironmentID) {
			cp := *r
			out = append(out, &cp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
