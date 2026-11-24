package service

import (
	"context"
	"fmt"
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

type ReminderService interface {
	List(ctx context.Context, req *dto.ListRemindersRequest) (*dto.ListRemindersResponse, error)
	Get(ctx context.Context, invoiceID string) (*dto.InvoiceReminderResponse, error)
	UpdateReminder(ctx context.Context, invoiceID string, req *dto.UpdateReminderRequest) (*dto.InvoiceReminderResponse, error)
	BulkUpdateReminders(ctx context.Context, req *dto.BulkUpdateRemindersRequest) (*dto.BulkUpdateRemindersResponse, error)
}

type reminderService struct {
	ServiceParams
}

func NewReminderService(params ServiceParams) ReminderService {
	return &reminderService{ServiceParams: params}
}

var reminderTransitions = map[types.ReminderStatus][]types.ReminderStatus{
	types.ReminderStatusPending:  {types.ReminderStatusApproved, types.ReminderStatusHeld, types.ReminderStatusSkipped},
	types.ReminderStatusHeld:     {types.ReminderStatusPending, types.ReminderStatusApproved, types.ReminderStatusSkipped},
	types.ReminderStatusApproved: {types.ReminderStatusPending},
	types.ReminderStatusSkipped:  {types.ReminderStatusPending},
}

var editableStatuses = []types.ReminderStatus{types.ReminderStatusPending, types.ReminderStatusHeld}

type reminderMail struct {
	tone    types.ReminderTone
	subject string
	body    string
}

type reminderCtx struct {
	now    time.Time
	tenant string
	emails map[string]*dto.ReminderEmailStatus
}

func reminderStatusOf(r collections.InvoiceWithReminder) types.ReminderStatus {
	if r.Reminder == nil {
		return types.ReminderStatusPending
	}
	return r.Reminder.ReminderStatus
}

func (s *reminderService) List(ctx context.Context, req *dto.ListRemindersRequest) (*dto.ListRemindersResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	asOf, err := parsePageAsOf(req.AsOf, clock.Now())
	if err != nil {
		return nil, err
	}
	filter := collections.InvoiceFilter{Column: types.CollectionsColumnOverdue, CustomerID: req.CustomerID}
	if req.Status != "" {
		filter.Statuses = []types.ReminderStatus{types.ReminderStatus(req.Status)}
	}
	invoices, hasMore, err := s.CollectionsRepo.ListInvoicesWithReminders(ctx, collections.Query{AsOf: asOf, Text: req.Q, Currency: normalizedCurrency(req.Currency)}, filter, req.PageOffset(), req.PageLimit())
	if err != nil {
		return nil, err
	}
	summary, err := s.CollectionsRepo.SummarizeReminders(ctx, asOf)
	if err != nil {
		return nil, err
	}
	customers, err := lookupCustomers(ctx, s.CustomerRepo, customerIDs(invoices))
	if err != nil {
		return nil, err
	}

	rc := reminderCtx{
		now:    asOf,
		tenant: s.tenantName(ctx),
	}
	if rc.emails, err = s.reminderEmails(ctx, invoices, false); err != nil {
		return nil, err
	}
	summaryReminder := dto.ReminderSummary{
		OverdueTotals: dtoTotals(summary.Totals),
		InvoiceCount:  summary.InvoiceCount,
		CustomerCount: summary.CustomerCount,
		ToReview:      summary.Statuses[types.ReminderStatusPending],
		Approved:      summary.Statuses[types.ReminderStatusApproved],
		Held:          summary.Statuses[types.ReminderStatusHeld],
		Skipped:       summary.Statuses[types.ReminderStatusSkipped],
	}
	resp := &dto.ListRemindersResponse{
		AsOf:    asOf,
		Summary: summaryReminder,
		Drafts:  []dto.InvoiceReminderResponse{},
		Page:    req.PageNumber(),
		HasMore: hasMore,
	}
	for _, r := range invoices {
		v, err := s.view(r, customers[r.CustomerID], rc)
		if err != nil {
			return nil, err
		}
		resp.Drafts = append(resp.Drafts, v)
	}
	return resp, nil
}

func (s *reminderService) Get(ctx context.Context, invoiceID string) (*dto.InvoiceReminderResponse, error) {
	invoices, customers, rc, err := s.load(ctx, []string{invoiceID})
	if err != nil {
		return nil, err
	}
	if rc.emails, err = s.reminderEmails(ctx, invoices, false); err != nil {
		return nil, err
	}
	v, err := s.view(invoices[0], customers[invoices[0].CustomerID], rc)
	return &v, err
}

func (s *reminderService) UpdateReminder(ctx context.Context, invoiceID string, req *dto.UpdateReminderRequest) (*dto.InvoiceReminderResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	invoices, customers, rc, err := s.load(ctx, []string{invoiceID})
	if err != nil {
		return nil, err
	}
	row, c := invoices[0], customers[invoices[0].CustomerID]
	next, err := s.plan(ctx, row, c, rc, req)
	if err != nil {
		return nil, err
	}
	if next != nil {
		if err := s.save(ctx, row, c, next); err != nil {
			return nil, err
		}
		row.Reminder = next
	}
	if rc.emails, err = s.reminderEmails(ctx, []collections.InvoiceWithReminder{row}, true); err != nil {
		return nil, err
	}
	v, err := s.view(row, c, rc)
	return &v, err
}

func (s *reminderService) BulkUpdateReminders(ctx context.Context, req *dto.BulkUpdateRemindersRequest) (*dto.BulkUpdateRemindersResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	invoices, customers, rc, err := s.load(ctx, lo.Uniq(req.InvoiceIDs))
	if err != nil {
		return nil, err
	}

	applied := map[string]collections.InvoiceWithReminder{}
	var inserts []*collections.InvoiceReminder

	for _, row := range invoices {
		next, err := s.plan(ctx, row, customers[row.CustomerID], rc, &req.UpdateReminderRequest)
		if errors.Is(err, ierr.ErrVersionConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}

		switch {
		case next == nil:
			applied[row.InvoiceID] = row

		case row.Reminder == nil && next.ReminderStatus != types.ReminderStatusApproved:
			inserts = append(inserts, next)

		default:
			err := s.save(ctx, row, customers[row.CustomerID], next)
			if errors.Is(err, ierr.ErrVersionConflict) {
				continue
			}
			if err != nil {
				return nil, err
			}

			row.Reminder = next
			applied[row.InvoiceID] = row
		}
	}

	created, err := s.InvoiceReminderRepo.CreateMany(ctx, inserts)
	if err != nil {
		return nil, err
	}

	rowsByInvoice := map[string]collections.InvoiceWithReminder{}
	for _, row := range invoices {
		rowsByInvoice[row.InvoiceID] = row
	}

	for _, next := range inserts {
		if !lo.Contains(created, next.InvoiceID) {
			continue
		}

		row := rowsByInvoice[next.InvoiceID]
		row.Reminder = next
		applied[row.InvoiceID] = row
	}

	appliedRows := make([]collections.InvoiceWithReminder, 0, len(applied))
	for _, row := range applied {
		appliedRows = append(appliedRows, row)
	}

	if rc.emails, err = s.reminderEmails(ctx, appliedRows, true); err != nil {
		return nil, err
	}

	resp := &dto.BulkUpdateRemindersResponse{Drafts: []dto.InvoiceReminderResponse{}}

	for _, row := range invoices {
		updated, ok := applied[row.InvoiceID]
		if !ok {
			continue
		}

		view, err := s.view(updated, customers[updated.CustomerID], rc)
		if err != nil {
			return nil, err
		}

		resp.Drafts = append(resp.Drafts, view)
	}

	return resp, nil
}

func (s *reminderService) load(ctx context.Context, ids []string) ([]collections.InvoiceWithReminder, map[string]*customer.Customer, reminderCtx, error) {
	rc := reminderCtx{now: clock.Now(), tenant: s.tenantName(ctx)}

	invoices, err := s.InvoiceReminderRepo.GetInvoicesWithReminders(ctx, rc.now, ids)
	if err != nil {
		return nil, nil, rc, err
	}

	found := map[string]collections.InvoiceWithReminder{}
	for _, row := range invoices {
		found[row.InvoiceID] = row
	}

	ordered := make([]collections.InvoiceWithReminder, 0, len(ids))
	for _, id := range ids {
		row, ok := found[id]
		if !ok {
			return nil, nil, rc, ierr.NewErrorf("reminder %s not found", id).
				WithHintf("Invoice %s was not found or is no longer open and overdue", id).
				Mark(ierr.ErrNotFound)
		}

		ordered = append(ordered, row)
	}

	customers, err := lookupCustomers(ctx, s.CustomerRepo, customerIDs(ordered))

	return ordered, customers, rc, err
}

func (s *reminderService) plan(ctx context.Context, row collections.InvoiceWithReminder, c *customer.Customer, rc reminderCtx, req *dto.UpdateReminderRequest) (*collections.InvoiceReminder, error) {
	current := reminderStatusOf(row)
	target := req.Status
	if target == "" {
		target = current
	}
	editsMail := req.Tone != nil || req.Subject != nil || req.Body != nil
	switch {
	case req.Status == current && !editsMail:
		return nil, reminderConflict("This reminder is already %s.", current)
	case target != current && !lo.Contains(reminderTransitions[current], target):
		return nil, reminderConflict("This reminder is %s, so it cannot be set to %s.", current, target)
	case editsMail && !lo.Contains(editableStatuses, current):
		return nil, reminderConflict("This reminder is %s, so its mail cannot be edited. Reopen it first.", current)
	}

	cur := row.Reminder
	shown, err := s.shown(row, c, rc)
	if err != nil {
		return nil, err
	}
	mail := shown
	if req.Tone != nil && types.ReminderTone(*req.Tone) != mail.tone {
		if mail, err = s.render(row, c, rc, types.ReminderTone(*req.Tone)); err != nil {
			return nil, err
		}
	}
	if req.Subject != nil {
		mail.subject = singleLine(*req.Subject)
	}
	if req.Body != nil {
		mail.body = *req.Body
	}
	reasonChanged := target == current && req.StatusReason != "" && (cur == nil || req.StatusReason != cur.StatusReason)
	if target == current && mail == shown && !reasonChanged {
		return nil, nil
	}

	if cur == nil || (target == types.ReminderStatusApproved && !editsMail && !cur.Edited()) {
		written, err := s.render(row, c, rc, "")
		if err != nil {
			return nil, err
		}
		if cur == nil {
			cur = collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{
				ID:        types.GenerateUUIDWithPrefix(types.UUID_PREFIX_INVOICE_REMINDER),
				InvoiceID: row.InvoiceID,
				BaseModel: types.GetDefaultBaseModel(ctx),
			}).WithStatus(types.ReminderStatusPending, "", nil).
				WithOriginal(written.tone, written.subject, written.body).
				WithText(written.tone, written.subject, written.body).Build()
		} else {
			cur = collections.NewInvoiceReminderBuilder(cur).
				WithOriginal(written.tone, written.subject, written.body).
				WithText(written.tone, written.subject, written.body).Build()
		}
		if !editsMail {
			mail = written
			shown = written
		}
	}
	if target == types.ReminderStatusApproved && current != target &&
		((req.ExpectedSubject != nil && *req.ExpectedSubject != mail.subject) || (req.ExpectedBody != nil && *req.ExpectedBody != mail.body)) {
		return nil, reminderConflict("The reminder text changed since you opened it. Review it again before approving.")
	}
	reason, approvedAt := cur.StatusReason, cur.ApprovedAt
	if reasonChanged {
		reason = req.StatusReason
	}
	if target != current {
		reason, approvedAt = req.StatusReason, nil
		if target == types.ReminderStatusApproved {
			approvedAt = lo.ToPtr(rc.now)
		}
	}
	next := collections.NewInvoiceReminderBuilder(cur)
	if mail != shown {
		next = next.WithText(mail.tone, mail.subject, mail.body)
	}
	return next.WithStatus(target, reason, approvedAt).Build(), nil
}

func (s *reminderService) save(ctx context.Context, row collections.InvoiceWithReminder, c *customer.Customer, next *collections.InvoiceReminder) error {
	if !changesApproval(row.Reminder, next) {
		return s.store(ctx, row, next)
	}
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		if err := s.cancelEmail(txCtx, row, next); err != nil {
			return err
		}
		if err := s.store(txCtx, row, next); err != nil {
			return err
		}
		return s.queueEmail(txCtx, row, c, next)
	})
}

func (s *reminderService) store(ctx context.Context, row collections.InvoiceWithReminder, next *collections.InvoiceReminder) error {
	var err error
	if row.Reminder == nil {
		err = s.InvoiceReminderRepo.Create(ctx, next)
	} else {
		err = s.InvoiceReminderRepo.Update(ctx, next, row.Reminder)
	}
	if errors.Is(err, ierr.ErrAlreadyExists) {
		return ierr.NewError("reminder changed").WithHint("This reminder was changed by someone else. Reload and try again.").Mark(ierr.ErrVersionConflict)
	}
	return err
}

// changesApproval reports a change that approves a reminder or takes its approval back.
func changesApproval(prev, next *collections.InvoiceReminder) bool {
	return next.ReminderStatus == types.ReminderStatusApproved || (prev != nil && prev.ReminderStatus == types.ReminderStatusApproved)
}

// queueEmail queues the email of a reminder that was just approved, with the text frozen as approved; the dispatcher sends it.
func (s *reminderService) queueEmail(ctx context.Context, row collections.InvoiceWithReminder, c *customer.Customer, next *collections.InvoiceReminder) error {
	if next.ReminderStatus != types.ReminderStatusApproved || next.ApprovedAt == nil {
		return nil
	}
	_, err := NewEmailNotificationService(s.ServiceParams).QueueReminderEmail(ctx, ReminderEmail{
		InvoiceID:  row.InvoiceID,
		CustomerID: row.CustomerID,
		ReminderID: next.ID,
		Recipient:  lo.FromPtr(c).Email,
		Subject:    next.Subject,
		Body:       next.Body,
		ApprovedAt: *next.ApprovedAt,
	})
	return err
}

// cancelEmail withdraws the email of an approval that is being taken back.
func (s *reminderService) cancelEmail(ctx context.Context, row collections.InvoiceWithReminder, next *collections.InvoiceReminder) error {
	prev := row.Reminder
	if prev == nil || prev.ReminderStatus != types.ReminderStatusApproved || prev.ApprovedAt == nil || next.ReminderStatus == types.ReminderStatusApproved {
		return nil
	}
	return NewEmailNotificationService(s.ServiceParams).CancelReminderEmail(ctx, reminderEmailKey(row.InvoiceID, prev.ID, *prev.ApprovedAt))
}

func (s *reminderService) render(r collections.InvoiceWithReminder, c *customer.Customer, rc reminderCtx, tone types.ReminderTone) (reminderMail, error) {
	if tone == "" {
		tone = reminderToneForDays(types.CalendarDaysBetween(*r.DueDate, rc.now))
	}
	greeting := ""
	if c != nil {
		greeting = strings.TrimSpace(c.Name)
	}
	subject, body, err := renderReminder(tone, greeting, rc.tenant, reminderInvoice{number: r.InvoiceNumber, currency: r.Currency, remaining: r.AmountRemaining, due: *r.DueDate}, rc.now)
	if err != nil {
		return reminderMail{}, ierr.WithError(err).WithHint("Failed to write the reminder").Mark(ierr.ErrInternal)
	}
	return reminderMail{tone: tone, subject: subject, body: body}, nil
}

func (s *reminderService) shown(r collections.InvoiceWithReminder, c *customer.Customer, rc reminderCtx) (reminderMail, error) {
	if rem := r.Reminder; rem != nil && (rem.Edited() || rem.ReminderStatus == types.ReminderStatusApproved) {
		return reminderMail{tone: rem.Tone, subject: rem.Subject, body: rem.Body}, nil
	}
	return s.render(r, c, rc, "")
}

func shownTone(r collections.InvoiceWithReminder, days int) types.ReminderTone {
	if rem := r.Reminder; rem != nil && (rem.Edited() || rem.ReminderStatus == types.ReminderStatusApproved) {
		return rem.Tone
	}
	return reminderToneForDays(days)
}

func (s *reminderService) view(r collections.InvoiceWithReminder, c *customer.Customer, rc reminderCtx) (dto.InvoiceReminderResponse, error) {
	mail, err := s.shown(r, c, rc)
	if err != nil {
		return dto.InvoiceReminderResponse{}, err
	}

	days := types.CalendarDaysBetween(*r.DueDate, rc.now)

	view := dto.InvoiceReminderResponse{
		ID:            r.InvoiceID,
		InvoiceID:     r.InvoiceID,
		InvoiceNumber: r.InvoiceNumber,
		CustomerID:    r.CustomerID,
		Currency:      r.Currency,
		Amount:        r.AmountRemaining,
		DueDate:       r.DueDate,
		DaysOverdue:   days,
		Status:        reminderStatusOf(r),
		Tone:          mail.tone,
		Subject:       mail.subject,
		Body:          mail.body,
	}
	if c != nil {
		view.CustomerName = c.Name
		view.CustomerEmail = c.Email
	}

	if rem := r.Reminder; rem != nil {
		view.StatusReason = rem.StatusReason
		view.ApprovedAt = rem.ApprovedAt
		view.CreatedAt = &rem.CreatedAt
		view.UpdatedAt = &rem.UpdatedAt

		view.Edited = rem.Edited()
		if view.Edited {
			original := rem.Original()
			view.OriginalTone = original.OriginalTone
			view.OriginalSubject = original.OriginalSubject
			view.OriginalBody = original.OriginalBody
		}
	}

	view.Email = rc.emails[r.InvoiceID]
	view.ToneReason = toneReason(view, days)

	return view, nil
}

// toneReason says why the reminder shows the tone it does: its age, the person who chose it, or the approval that froze it.
func toneReason(v dto.InvoiceReminderResponse, days int) string {
	age := fmt.Sprintf("invoice is %s", overdueLabel(days))
	if days == 0 {
		age = "invoice became overdue today"
	}

	switch {
	case v.Status == types.ReminderStatusApproved:
		return fmt.Sprintf("Approved with a %s tone, kept as approved (%s).", reminderToneLabel(v.Tone), overdueLabel(days))

	case v.Edited && v.Tone != v.OriginalTone:
		return fmt.Sprintf("Tone set to %s by you (%s).", reminderToneLabel(v.Tone), age)
	}

	return fmt.Sprintf("%s, so a %s tone.", strings.ToUpper(age[:1])+age[1:], reminderToneLabel(reminderToneForDays(days)))
}

func (s *reminderService) tenantName(ctx context.Context) string {
	t, err := s.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil || t == nil {
		return ""
	}
	return strings.TrimSpace(t.Name)
}

func reminderConflict(hint string, args ...any) error {
	return ierr.NewErrorf("reminder cannot change").WithHintf(hint, args...).Mark(ierr.ErrVersionConflict)
}
