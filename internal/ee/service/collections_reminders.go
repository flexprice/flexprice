package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// ReminderService shows payment reminders for overdue invoices and records the decision on each.
// Reminders are rendered when read; only decisions and edits are stored. Approving sends nothing.
type ReminderService interface {
	List(ctx context.Context, req *dto.ListRemindersRequest) (*dto.ListRemindersResponse, error)
	Get(ctx context.Context, id string) (*dto.ReminderDraftResponse, error)
	Update(ctx context.Context, id string, req *dto.UpdateReminderRequest) (*dto.ReminderDraftResponse, error)
	Approve(ctx context.Context, id string) (*dto.ReminderDraftResponse, error)
	Hold(ctx context.Context, id string, req *dto.HoldReminderRequest) (*dto.ReminderDraftResponse, error)
	Skip(ctx context.Context, id string) (*dto.ReminderDraftResponse, error)
	Reopen(ctx context.Context, id string) (*dto.ReminderDraftResponse, error)
	BulkApprove(ctx context.Context, req *dto.BulkApproveRemindersRequest) (*dto.BulkApproveRemindersResponse, error)
}

type reminderService struct {
	ServiceParams
}

func NewReminderService(params ServiceParams) ReminderService {
	return &reminderService{ServiceParams: params}
}

// reminderEntry is one live overdue group with the decision a person recorded on it, if any.
type reminderEntry struct {
	id       string
	group    overdueGroup
	decision *collections.ReminderDecision
	// stored is the row of this customer and currency even when it no longer applies; a new decision takes its place.
	stored   *collections.ReminderDecision
	customer *customer.Customer
}

type reminderState struct {
	now     time.Time
	tenant  string
	entries []*reminderEntry
}

func reminderKey(customerID, currency string) string { return customerID + "|" + currency }

func invoiceSetHash(rows []collections.Receivable) string {
	lines := lo.Map(rows, func(r collections.Receivable, _ int) string {
		return fmt.Sprintf("%s|%s|%d", r.ExternalID, r.AmountRemaining.String(), r.DueDate.Unix())
	})
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:])
}

func groupInvoiceIDs(g overdueGroup) []string {
	return lo.Map(g.rows, func(r collections.Receivable, _ int) string { return r.ExternalID })
}

// decisionsByGroup indexes decisions by customer and currency.
func decisionsByGroup(decisions []*collections.ReminderDecision) map[string]*collections.ReminderDecision {
	byKey := make(map[string]*collections.ReminderDecision, len(decisions))
	for _, d := range decisions {
		byKey[reminderKey(d.CustomerID, d.Currency)] = d
	}
	return byKey
}

// decisionFor is the decision that applies to a group. One made on invoices that are all gone no longer applies,
// so an old hold cannot reappear on new invoices of the same customer.
func decisionFor(byKey map[string]*collections.ReminderDecision, g overdueGroup) *collections.ReminderDecision {
	d := byKey[reminderKey(g.customerID, g.currency)]
	if d == nil || !lo.Some(d.InvoiceIDs, groupInvoiceIDs(g)) {
		return nil
	}
	return d
}

func statusOf(e *reminderEntry) types.ReminderStatus {
	if e.decision == nil {
		return types.ReminderStatusPending
	}
	return e.decision.Status
}

// loadEntries reads the overdue receivables of the named groups and the decisions made on them; it never writes.
func (s *reminderService) loadEntries(ctx context.Context, asOf time.Time, keys []collections.GroupKey) (*reminderState, error) {
	state := &reminderState{now: asOf}
	if len(keys) == 0 {
		return state, nil
	}
	rows, err := s.ReceivableSource.GroupInvoices(ctx, asOf, keys)
	if err != nil {
		return nil, err
	}
	decisions, err := s.ReminderDecisionRepo.ListByKeys(ctx, keys)
	if err != nil {
		return nil, err
	}
	customers, err := lookupCustomers(ctx, s.CustomerRepo, lo.Uniq(lo.Map(keys, func(k collections.GroupKey, _ int) string { return k.CustomerID })))
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if c := customers[rows[i].Customer.ID]; c != nil {
			rows[i].Customer.Name, rows[i].Customer.Email = c.Name, c.Email
		}
	}

	byKey := decisionsByGroup(decisions)
	state.tenant = s.tenantName(ctx)
	for _, g := range groupReceivables(rows) {
		state.entries = append(state.entries, &reminderEntry{
			id:       formatReminderID(g.currency, g.customerID),
			group:    g,
			decision: decisionFor(byKey, g),
			stored:   byKey[reminderKey(g.customerID, g.currency)],
			customer: customers[g.customerID],
		})
	}
	return state, nil
}

// loadOne reads one reminder by id; a malformed id or a group that is not overdue is not found.
func (s *reminderService) loadOne(ctx context.Context, id string) (*reminderState, *reminderEntry, error) {
	currency, customerID, ok := parseReminderID(id)
	if !ok {
		return nil, nil, reminderNotFound(id)
	}
	state, err := s.loadEntries(ctx, clock.Now(), []collections.GroupKey{{CustomerID: customerID, Currency: currency}})
	if err != nil {
		return nil, nil, err
	}
	entry, err := state.find(id)
	return state, entry, err
}

func reminderNotFound(id string) error {
	return ierr.NewErrorf("reminder %s not found", id).WithHintf("Reminder %s was not found or its invoices are no longer overdue", id).Mark(ierr.ErrNotFound)
}

func (s *reminderService) tenantName(ctx context.Context) string {
	t, err := s.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil || t == nil {
		return ""
	}
	return strings.TrimSpace(t.Name)
}

func (st *reminderState) find(id string) (*reminderEntry, error) {
	if e, found := lo.Find(st.entries, func(e *reminderEntry) bool { return e.id == id }); found {
		return e, nil
	}
	return nil, reminderNotFound(id)
}

func reminderGreeting(e *reminderEntry) string {
	if e.customer != nil && strings.TrimSpace(e.customer.Name) != "" {
		return strings.TrimSpace(e.customer.Name)
	}
	return e.group.rows[0].Customer.Name
}

func autoReminderTone(e *reminderEntry, now time.Time) types.ReminderTone {
	return reminderToneForDays(e.group.oldestDaysOverdue(now))
}

// liveText is the tone, subject and body as they would read now for the given decision: overrides win over the template.
func (st *reminderState) liveText(e *reminderEntry, d *collections.ReminderDecision) (tone types.ReminderTone, subject, body string, err error) {
	tone = autoReminderTone(e, st.now)
	if d != nil && d.ToneOverride != "" {
		tone = d.ToneOverride
	}
	subject, body, err = renderReminder(tone, reminderGreeting(e), st.tenant, e.group, st.now)
	if err != nil {
		return "", "", "", ierr.WithError(err).WithHint("Failed to write the reminder").Mark(ierr.ErrInternal)
	}
	if d != nil && d.SubjectOverride != "" {
		subject = d.SubjectOverride
	}
	if d != nil && d.BodyOverride != "" {
		body = d.BodyOverride
	}
	return tone, subject, body, nil
}

func (st *reminderState) view(e *reminderEntry) (dto.ReminderDraftResponse, error) {
	d := e.decision
	tone, subject, body, err := st.liveText(e, d)
	if err != nil {
		return dto.ReminderDraftResponse{}, err
	}
	if d != nil && d.Status == types.ReminderStatusApproved && d.FrozenSubject != "" {
		tone, subject, body = d.FrozenTone, d.FrozenSubject, d.FrozenBody
	}

	g := e.group
	view := dto.ReminderDraftResponse{
		ID:                e.id,
		CustomerID:        g.customerID,
		CustomerName:      g.rows[0].Customer.Name,
		Currency:          g.currency,
		Status:            statusOf(e),
		Tone:              tone,
		Subject:           subject,
		Body:              body,
		Amount:            g.amount(),
		InvoiceCount:      len(g.rows),
		OldestDaysOverdue: g.oldestDaysOverdue(st.now),
		Invoices: lo.Map(g.rows, func(r collections.Receivable, _ int) dto.ReminderInvoice {
			return dto.ReminderInvoice{
				ID:              r.ExternalID,
				Number:          r.Number,
				AmountRemaining: r.AmountRemaining,
				DueDate:         r.DueDate,
				DaysOverdue:     calendarDaysBetween(*r.DueDate, st.now),
			}
		}),
	}
	if e.customer != nil {
		view.CustomerName, view.CustomerEmail = e.customer.Name, e.customer.Email
	}
	if d != nil {
		view.HoldReason, view.DecidedAt = d.HoldReason, d.DecidedAt
		view.Edited = d.Edited()
		view.Stale = d.InvoiceSetHash != invoiceSetHash(g.rows) && (d.Status != types.ReminderStatusPending || d.Edited())
		view.CreatedAt, view.UpdatedAt = &d.CreatedAt, &d.UpdatedAt
	}
	view.Why = reminderWhy(e, view, st.now)
	return view, nil
}

func reminderWhy(e *reminderEntry, v dto.ReminderDraftResponse, now time.Time) []string {
	why := []string{}
	ageText := fmt.Sprintf("oldest invoice is %d days overdue", v.OldestDaysOverdue)
	if v.OldestDaysOverdue == 0 {
		ageText = "oldest invoice became overdue today"
	}
	if e.decision != nil && e.decision.ToneOverride != "" {
		why = append(why, fmt.Sprintf("Tone set to %s by you (%s).", reminderToneLabel(e.decision.ToneOverride), ageText))
	} else {
		why = append(why, fmt.Sprintf("%s, so a %s tone.", strings.ToUpper(ageText[:1])+ageText[1:], reminderToneLabel(autoReminderTone(e, now))))
	}
	why = append(why, fmt.Sprintf("%s covered in one email, %s in total.", pluralInvoices(v.InvoiceCount), reminderMoney(v.Amount, v.Currency)))
	for _, r := range e.group.rows {
		if r.AmountRemaining.LessThan(r.AmountTotal) {
			why = append(why, fmt.Sprintf("%s is partly paid, so only the remaining %s is requested.", r.Number, reminderMoney(r.AmountRemaining, v.Currency)))
		}
	}
	if v.Edited {
		why = append(why, "You edited this draft, so it is not rewritten automatically.")
	}
	if v.Stale {
		why = append(why, "The invoices changed after this draft was written.")
	}
	if v.HoldReason != "" {
		why = append(why, "Held: "+v.HoldReason)
	}
	return why
}

func pluralInvoices(n int) string {
	if n == 1 {
		return "1 invoice"
	}
	return fmt.Sprintf("%d invoices", n)
}

func toneCutoffs(asOf time.Time) collections.ToneCutoffs {
	today := utcDay(asOf)
	return collections.ToneCutoffs{
		Friendly: today.AddDate(0, 0, -reminderFriendlyMaxDays),
		Firm:     today.AddDate(0, 0, -reminderFirmMaxDays),
	}
}

func (s *reminderService) List(ctx context.Context, req *dto.ListRemindersRequest) (*dto.ListRemindersResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	asOf, err := parsePageAsOf(req.AsOf, clock.Now())
	if err != nil {
		return nil, err
	}

	filter := collections.GroupFilter{
		Tone:        types.ReminderTone(req.Tone),
		ToneCutoff:  toneCutoffs(asOf),
		CustomerID:  req.CustomerID,
		WholeGroups: true,
	}
	if req.Status != "" {
		filter.Statuses = []types.ReminderStatus{types.ReminderStatus(req.Status)}
	}
	groups, hasMore, err := s.ReceivableSource.ListGroups(ctx, collections.Query{AsOf: asOf, Text: req.Q, Currency: normalizedCurrency(req.Currency)}, filter, req.PageOffset(), req.PageLimit())
	if err != nil {
		return nil, err
	}
	summary, err := s.ReceivableSource.SummarizeGroups(ctx, asOf)
	if err != nil {
		return nil, err
	}
	keys := lo.Map(groups, func(g collections.ReceivableGroup, _ int) collections.GroupKey {
		return collections.GroupKey{CustomerID: g.CustomerID, Currency: normalizedCurrency(g.Currency)}
	})
	state, err := s.loadEntries(ctx, asOf, keys)
	if err != nil {
		return nil, err
	}

	resp := &dto.ListRemindersResponse{AsOf: asOf, Summary: summarizeReminders(summary), Drafts: []dto.ReminderDraftResponse{}, Page: req.PageNumber(), HasMore: hasMore}
	for _, g := range groups {
		entry, found := lo.Find(state.entries, func(e *reminderEntry) bool {
			return e.id == formatReminderID(normalizedCurrency(g.Currency), g.CustomerID)
		})
		if !found {
			continue
		}
		view, err := state.view(entry)
		if err != nil {
			return nil, err
		}
		if len(view.Invoices) > dto.ReminderListInvoiceCap {
			view.Invoices = view.Invoices[:dto.ReminderListInvoiceCap]
		}
		resp.Drafts = append(resp.Drafts, view)
	}
	return resp, nil
}

func summarizeReminders(g collections.GroupSummary) dto.ReminderSummary {
	return dto.ReminderSummary{
		OverdueTotals: dtoTotals(g.Totals),
		InvoiceCount:  g.InvoiceCount,
		CustomerCount: g.CustomerCount,
		ToReview:      g.Statuses[types.ReminderStatusPending],
		Approved:      g.Statuses[types.ReminderStatusApproved],
		Held:          g.Statuses[types.ReminderStatusHeld],
		Skipped:       g.Statuses[types.ReminderStatusSkipped],
	}
}

func (s *reminderService) Get(ctx context.Context, id string) (*dto.ReminderDraftResponse, error) {
	state, entry, err := s.loadOne(ctx, id)
	if err != nil {
		return nil, err
	}
	view, err := state.view(entry)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func reminderConflict(status types.ReminderStatus, action string) error {
	return ierr.NewErrorf("cannot %s a %s reminder", action, status).
		WithHintf("A %s reminder cannot be %s.", status, action).
		Mark(ierr.ErrVersionConflict)
}

type reminderOutcome int

const (
	reminderWrite reminderOutcome = iota
	reminderDrop
	reminderNoChange
)

type reminderChange func(next *collections.ReminderDecision, e *reminderEntry, st *reminderState) (reminderOutcome, error)

var (
	reviewable = []types.ReminderStatus{types.ReminderStatusPending, types.ReminderStatusHeld}
	decided    = []types.ReminderStatus{types.ReminderStatusApproved, types.ReminderStatusHeld, types.ReminderStatusSkipped}
)

// refreshInvoices records the invoices the decision is now made on, unless edited text still refers to older ones.
func refreshInvoices(d *collections.ReminderDecision, e *reminderEntry) {
	if d.Edited() {
		return
	}
	d.InvoiceIDs, d.InvoiceSetHash = groupInvoiceIDs(e.group), invoiceSetHash(e.group.rows)
}

// applyChange builds the next decision for an entry, writes it with the version it was read at and returns the new view.
func (s *reminderService) applyChange(ctx context.Context, st *reminderState, e *reminderEntry, change reminderChange) (*dto.ReminderDraftResponse, error) {
	existing := e.decision
	next := &collections.ReminderDecision{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_REMINDER_DECISION),
		CustomerID:     e.group.customerID,
		Currency:       e.group.currency,
		Status:         types.ReminderStatusPending,
		InvoiceIDs:     groupInvoiceIDs(e.group),
		InvoiceSetHash: invoiceSetHash(e.group.rows),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	if existing != nil {
		cp := *existing
		next = &cp
	} else if e.stored != nil {
		next.ID, next.Version = e.stored.ID, e.stored.Version
	}

	outcome, err := change(next, e, st)
	if err != nil {
		return nil, err
	}
	result := *e
	switch {
	case outcome == reminderDrop && existing != nil:
		err = s.ReminderDecisionRepo.Delete(ctx, existing)
		result.decision = nil
	case outcome == reminderDrop || outcome == reminderNoChange:
	case existing == nil && e.stored == nil:
		err = s.ReminderDecisionRepo.Create(ctx, next)
		result.decision = next
	default:
		err = s.ReminderDecisionRepo.Update(ctx, next)
		result.decision = next
	}
	if err != nil {
		return nil, err
	}
	view, err := st.view(&result)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

func isWriteConflict(err error) bool {
	return errors.Is(err, ierr.ErrVersionConflict) || errors.Is(err, ierr.ErrAlreadyExists)
}

// mutate runs one change on a reminder. A concurrent change fails the write; the whole step then runs once more on fresh data.
func (s *reminderService) mutate(ctx context.Context, id, action string, allowed []types.ReminderStatus, change reminderChange) (*dto.ReminderDraftResponse, error) {
	for attempt := 0; ; attempt++ {
		state, entry, err := s.loadOne(ctx, id)
		if err != nil {
			return nil, err
		}
		if status := statusOf(entry); !lo.Contains(allowed, status) {
			return nil, reminderConflict(status, action)
		}
		view, err := s.applyChange(ctx, state, entry, change)
		if err != nil && isWriteConflict(err) && attempt == 0 {
			continue
		}
		return view, err
	}
}

func approveChange(next *collections.ReminderDecision, e *reminderEntry, st *reminderState) (reminderOutcome, error) {
	tone, subject, body, err := st.liveText(e, next)
	if err != nil {
		return reminderWrite, err
	}
	refreshInvoices(next, e)
	next.Status, next.HoldReason, next.DecidedAt = types.ReminderStatusApproved, "", lo.ToPtr(st.now)
	next.FrozenTone, next.FrozenSubject, next.FrozenBody = tone, subject, body
	return reminderWrite, nil
}

func (s *reminderService) Approve(ctx context.Context, id string) (*dto.ReminderDraftResponse, error) {
	return s.mutate(ctx, id, "approved", reviewable, approveChange)
}

func (s *reminderService) Hold(ctx context.Context, id string, req *dto.HoldReminderRequest) (*dto.ReminderDraftResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, "held", []types.ReminderStatus{types.ReminderStatusPending}, func(next *collections.ReminderDecision, e *reminderEntry, st *reminderState) (reminderOutcome, error) {
		refreshInvoices(next, e)
		next.Status, next.HoldReason, next.DecidedAt = types.ReminderStatusHeld, req.Reason, lo.ToPtr(st.now)
		return reminderWrite, nil
	})
}

func (s *reminderService) Skip(ctx context.Context, id string) (*dto.ReminderDraftResponse, error) {
	return s.mutate(ctx, id, "skipped", reviewable, func(next *collections.ReminderDecision, e *reminderEntry, st *reminderState) (reminderOutcome, error) {
		refreshInvoices(next, e)
		next.Status, next.HoldReason, next.DecidedAt = types.ReminderStatusSkipped, "", lo.ToPtr(st.now)
		return reminderWrite, nil
	})
}

func (s *reminderService) Reopen(ctx context.Context, id string) (*dto.ReminderDraftResponse, error) {
	return s.mutate(ctx, id, "reopened", decided, func(next *collections.ReminderDecision, e *reminderEntry, _ *reminderState) (reminderOutcome, error) {
		next.Status, next.HoldReason, next.DecidedAt = types.ReminderStatusPending, "", nil
		next.FrozenTone, next.FrozenSubject, next.FrozenBody = "", "", ""
		if !next.Edited() && next.ToneOverride == "" {
			return reminderDrop, nil
		}
		refreshInvoices(next, e)
		return reminderWrite, nil
	})
}

func (s *reminderService) Update(ctx context.Context, id string, req *dto.UpdateReminderRequest) (*dto.ReminderDraftResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return s.mutate(ctx, id, "edited", reviewable, func(next *collections.ReminderDecision, e *reminderEntry, st *reminderState) (reminderOutcome, error) {
		changed := false
		if !next.Edited() {
			refreshInvoices(next, e)
		}
		current, _, _, err := st.liveText(e, next)
		if err != nil {
			return reminderWrite, err
		}
		if req.Tone != nil && types.ReminderTone(*req.Tone) != current {
			next.ToneOverride = types.ReminderTone(*req.Tone)
			if next.ToneOverride == autoReminderTone(e, st.now) {
				next.ToneOverride = ""
			}
			next.SubjectOverride, next.BodyOverride = "", ""
			refreshInvoices(next, e)
			changed = true
		}
		_, subject, body, err := st.liveText(e, next)
		if err != nil {
			return reminderWrite, err
		}
		if req.Subject != nil && strings.TrimSpace(*req.Subject) != subject {
			next.SubjectOverride, changed = strings.TrimSpace(*req.Subject), true
		}
		if req.Body != nil && *req.Body != body {
			next.BodyOverride, changed = *req.Body, true
		}
		switch {
		case !changed:
			return reminderNoChange, nil
		case next.Status == types.ReminderStatusPending && !next.Edited() && next.ToneOverride == "":
			return reminderDrop, nil
		}
		return reminderWrite, nil
	})
}

func (s *reminderService) BulkApprove(ctx context.Context, req *dto.BulkApproveRemindersRequest) (*dto.BulkApproveRemindersResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	ids := lo.Uniq(req.IDs)
	keys := make([]collections.GroupKey, 0, len(ids))
	for _, id := range ids {
		currency, customerID, ok := parseReminderID(id)
		if !ok {
			return nil, reminderNotFound(id)
		}
		keys = append(keys, collections.GroupKey{CustomerID: customerID, Currency: currency})
	}
	state, err := s.loadEntries(ctx, clock.Now(), keys)
	if err != nil {
		return nil, err
	}
	entries := make([]*reminderEntry, 0, len(ids))
	for _, id := range ids {
		entry, err := state.find(id)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}

	resp := &dto.BulkApproveRemindersResponse{Drafts: []dto.ReminderDraftResponse{}}
	for _, entry := range entries {
		if statusOf(entry) != types.ReminderStatusPending {
			continue
		}
		view, err := s.applyChange(ctx, state, entry, approveChange)
		if isWriteConflict(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		resp.Drafts = append(resp.Drafts, *view)
	}
	return resp, nil
}
