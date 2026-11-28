package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/samber/lo"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

const (
	// playbookEnrolPage is how many invoices one candidate query returns.
	playbookEnrolPage = 500
	// playbookEnrolMaxPages caps the invoices one playbook enrols per pass; the rest wait for the next pass.
	playbookEnrolMaxPages = 20
	// playbookDuePage is how many due runs one query returns.
	playbookDuePage = 200
)

// RunPlaybooks runs one scheduler pass in each environment holding a live playbook, each in its own scoped
// context. An environment that fails does not stop the others; their errors are returned together.
func (s *playbookService) RunPlaybooks(ctx context.Context) error {
	envs, err := s.PlaybookRepo.ListLiveEnvironments(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, env := range envs {
		envCtx := types.SetEnvironmentID(types.SetTenantID(ctx, env.TenantID), env.EnvironmentID)
		resp, err := s.RunEnvironment(envCtx)
		if ierr.IsPermissionDenied(err) {
			// Collections was turned off with playbooks still live: nothing runs until it is on again.
			s.Logger.Info(envCtx, "playbooks skipped: collections is off", "tenant_id", env.TenantID, "environment_id", env.EnvironmentID)
			continue
		}
		if err != nil {
			s.Logger.Error(envCtx, "playbook pass failed", "error", err, "tenant_id", env.TenantID, "environment_id", env.EnvironmentID)
			errs = append(errs, err)
			continue
		}
		s.Logger.Info(envCtx, "playbook pass done", "tenant_id", env.TenantID, "environment_id", env.EnvironmentID,
			"runs_stopped", resp.RunsStopped, "runs_enrolled", resp.RunsEnrolled, "steps_executed", resp.StepsExecuted,
			"steps_skipped", resp.StepsSkipped, "steps_failed", resp.StepsFailed, "steps_deferred", resp.StepsDeferred)
	}
	return errors.Join(errs...)
}

// RunEnvironment stops runs whose invoice was paid or voided, enrols newly matching invoices into the first live
// playbook they match, then runs every step that is due. Each step is its own unit of work keyed by (run, step), so
// a retried pass never repeats a step.
func (s *playbookService) RunEnvironment(ctx context.Context) (*dto.RunPlaybooksResponse, error) {
	if err := requireCollectionsEnabled(ctx, s.ServiceParams); err != nil {
		return nil, err
	}
	now := clock.Now()
	resp := &dto.RunPlaybooksResponse{}

	stopped, err := s.PlaybookRunRepo.StopFinishedRuns(ctx, now)
	if err != nil {
		return nil, err
	}
	resp.RunsStopped = stopped

	live, err := s.PlaybookRepo.List(ctx, collections.PlaybookFilter{States: []types.PlaybookState{types.PlaybookStateLive}})
	if err != nil {
		return nil, err
	}
	for _, p := range live {
		n, err := s.enrol(ctx, p, now)
		resp.RunsEnrolled += n
		if err != nil {
			return resp, err
		}
	}

	playbooks := lo.SliceToMap(live, func(p *collections.Playbook) (string, *collections.Playbook) { return p.ID, p })
	seen := map[string]bool{}
	for {
		ids, err := s.PlaybookRunRepo.ListDueRunIDs(ctx, now, playbookDuePage)
		if err != nil {
			return resp, err
		}
		fresh := lo.Filter(ids, func(id string, _ int) bool { return !seen[id] })
		if len(fresh) == 0 {
			break
		}
		for _, id := range fresh {
			seen[id] = true
			if err := s.executeRun(ctx, id, playbooks, now, resp); err != nil {
				return resp, err
			}
		}
	}
	return resp, nil
}

// enrol starts p for every invoice it may take now, scheduling only the steps whose day is today or later.
func (s *playbookService) enrol(ctx context.Context, p *collections.Playbook, now time.Time) (int, error) {
	if len(p.Steps) == 0 {
		return 0, nil
	}
	today := types.UTCDay(now)
	enrolled := 0
	for page := 0; page < playbookEnrolMaxPages; page++ {
		candidates, err := s.PlaybookRunRepo.ListCandidates(ctx, p, now, playbookEnrolPage)
		if err != nil {
			return enrolled, err
		}
		for _, c := range candidates {
			run, steps := newRun(ctx, p, c, today, now)
			if len(steps) == 0 {
				continue
			}
			err := s.PlaybookRunRepo.CreateRun(ctx, run, steps)
			if isAlreadyExists(err) {
				// Another pass enrolled it meanwhile.
				continue
			}
			if err != nil {
				return enrolled, err
			}
			enrolled++
		}
		if len(candidates) < playbookEnrolPage {
			break
		}
	}
	return enrolled, nil
}

// newRun builds a run of p for the invoice with a step run for each step due today or later.
func newRun(ctx context.Context, p *collections.Playbook, c collections.PlaybookCandidate, today, now time.Time) (*collections.PlaybookRun, []*collections.PlaybookStepRun) {
	run := &collections.PlaybookRun{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COLLECTION_PLAYBOOK_RUN),
		PlaybookID:     p.ID,
		InvoiceID:      c.InvoiceID,
		CustomerID:     c.CustomerID,
		State:          types.PlaybookRunActive,
		InvoiceDueDate: c.DueDate,
		StartedAt:      now,
		EnvironmentID:  types.GetEnvironmentID(ctx),
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	var steps []*collections.PlaybookStepRun
	for _, step := range p.OrderedSteps() {
		due := types.PlaybookStepDueOn(c.DueDate, step.Day)
		if due.Before(today) {
			continue
		}
		steps = append(steps, newStepRun(ctx, run.ID, step, due))
	}
	if len(steps) > 0 {
		run.NextStepAt = lo.ToPtr(steps[0].DueAt)
	}
	return run, steps
}

// stepOutcome is what running one step did.
type stepOutcome struct {
	ref        string
	skipReason types.PlaybookStepSkipReason
	// deferTo moves the step to a later day instead of running it.
	deferTo *time.Time
}

// executeRun runs the run's due steps. Steps missed on earlier days are superseded by the latest day's, so a run
// that fell behind sends one step rather than a burst.
func (s *playbookService) executeRun(ctx context.Context, runID string, playbooks map[string]*collections.Playbook, now time.Time, resp *dto.RunPlaybooksResponse) error {
	run, err := s.PlaybookRunRepo.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	p, ok := playbooks[run.PlaybookID]
	if !ok || run.State != types.PlaybookRunActive {
		return nil
	}
	steps, err := s.PlaybookRunRepo.ListStepRuns(ctx, []string{run.ID})
	if err != nil {
		return err
	}
	for _, sr := range supersededSteps(steps, now) {
		if err := s.finishStep(ctx, sr, stepOutcome{skipReason: types.PlaybookSkipSuperseded}, nil, resp); err != nil {
			return err
		}
	}
	steps, err = s.PlaybookRunRepo.ListStepRuns(ctx, []string{run.ID})
	if err != nil {
		return err
	}
	for _, sr := range steps {
		if sr.State != types.PlaybookStepRunPending || sr.DueAt.After(now) {
			continue
		}
		step, ok := p.Step(sr.StepID)
		if !ok {
			if err := s.finishStep(ctx, sr, stepOutcome{skipReason: types.PlaybookSkipEditedOut}, nil, resp); err != nil {
				return err
			}
			continue
		}
		outcome, stepErr := s.executeStep(ctx, p, run, sr, step, now)
		if err := s.finishStep(ctx, sr, outcome, stepErr, resp); err != nil {
			return err
		}
	}
	return s.advanceRun(ctx, run)
}

// finishStep records what a step did. A failed step stays pending for the next pass until it has failed
// PlaybookStepRunMaxAttempts times; then it is left failed with the error.
func (s *playbookService) finishStep(ctx context.Context, sr *collections.PlaybookStepRun, outcome stepOutcome, stepErr error, resp *dto.RunPlaybooksResponse) error {
	next := *sr
	now := clock.Now()
	switch {
	case stepErr != nil:
		next.Attempts++
		next.Error = errors.FlattenHints(stepErr)
		if next.Error == "" {
			next.Error = stepErr.Error()
		}
		if next.Attempts >= types.PlaybookStepRunMaxAttempts {
			next.State, next.ExecutedAt = types.PlaybookStepRunFailed, &now
		}
		resp.StepsFailed++
		s.Logger.Error(ctx, "playbook step failed", "error", stepErr, "run_id", sr.RunID, "step_id", sr.StepID, "attempts", next.Attempts)
	case outcome.deferTo != nil:
		next.DueAt = *outcome.deferTo
		resp.StepsDeferred++
	case outcome.skipReason != "":
		next.State, next.SkipReason, next.ExecutedAt = types.PlaybookStepRunSkipped, outcome.skipReason, &now
		resp.StepsSkipped++
	default:
		next.State, next.ResultRef, next.ExecutedAt, next.Error = types.PlaybookStepRunDone, outcome.ref, &now, ""
		resp.StepsExecuted++
	}
	err := s.PlaybookRunRepo.UpdateStepRun(ctx, &next, types.PlaybookStepRunPending)
	if ierr.IsVersionConflict(err) {
		// Another pass finished this step first.
		return nil
	}
	return err
}

// advanceRun points the run at its next pending step, or completes it when none is left.
func (s *playbookService) advanceRun(ctx context.Context, run *collections.PlaybookRun) error {
	steps, err := s.PlaybookRunRepo.ListStepRuns(ctx, []string{run.ID})
	if err != nil {
		return err
	}
	cur, err := s.PlaybookRunRepo.GetRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if !cur.IsLive() {
		return nil
	}
	next := *cur
	if step := collections.NextStepByRun(steps)[run.ID]; step != nil {
		if next.NextStepAt != nil && next.NextStepAt.Equal(step.DueAt) {
			return nil
		}
		next.NextStepAt = lo.ToPtr(step.DueAt)
	} else {
		now := clock.Now()
		next.State, next.NextStepAt, next.EndedAt = types.PlaybookRunCompleted, nil, &now
	}
	err = s.PlaybookRunRepo.UpdateRun(ctx, &next)
	if ierr.IsVersionConflict(err) {
		return nil
	}
	return err
}

// executeStep runs one due step. A returned error is retried by a later pass; a skip is final.
func (s *playbookService) executeStep(ctx context.Context, p *collections.Playbook, run *collections.PlaybookRun, sr *collections.PlaybookStepRun, step types.PlaybookStep, now time.Time) (stepOutcome, error) {
	inv, err := s.InvoiceRepo.Get(ctx, run.InvoiceID)
	if err != nil {
		return stepOutcome{}, err
	}
	if !invoiceStillOwed(inv.InvoiceStatus, inv.PaymentStatus, inv.AmountRemaining.IsPositive()) {
		// The next pass stops the run; this step does not go out.
		return stepOutcome{skipReason: types.PlaybookSkipRunStopped}, nil
	}
	c, err := s.CustomerRepo.Get(ctx, run.CustomerID)
	if err != nil {
		return stepOutcome{}, err
	}
	vars := s.playbookVariables(ctx, inv, c, now)

	switch step.Kind {
	case types.PlaybookStepEmail:
		return s.executeEmailStep(ctx, p, run, sr, step, inv, c, vars, now)
	case types.PlaybookStepInternal:
		if step.Channel == types.PlaybookChannelSlack {
			return stepOutcome{skipReason: types.PlaybookSkipSlackNotConnected}, nil
		}
		return s.queueNotice(ctx, p, run, sr, inv, []string{step.To}, "Alert", fillPlaybookText(step.Note, vars))
	case types.PlaybookStepTask:
		return s.executeTaskStep(ctx, p, run, sr, step, inv, vars)
	}
	return stepOutcome{}, ierr.NewErrorf("unknown step kind %s", step.Kind).Mark(ierr.ErrValidation)
}

func (s *playbookService) executeEmailStep(ctx context.Context, p *collections.Playbook, run *collections.PlaybookRun, sr *collections.PlaybookStepRun, step types.PlaybookStep, inv *invoice.Invoice, c *customer.Customer, vars map[string]string, now time.Time) (stepOutcome, error) {
	if day, ok := s.emailGuardrail(ctx, p, run, now); !ok {
		return stepOutcome{deferTo: &day}, nil
	}
	mail := reminderMail{tone: step.Tone, subject: singleLine(fillPlaybookText(step.Subject, vars)), body: fillPlaybookText(step.Body, vars)}

	if types.IsOverdue(inv.DueDate, now) {
		reminders := &reminderService{ServiceParams: s.ServiceParams}
		reminder, held, err := reminders.applyPlaybookStep(ctx, inv.ID, c, mail, p.ID, step.ID, step.SendWithoutReview)
		switch {
		case ierr.IsNotFound(err):
			return stepOutcome{skipReason: types.PlaybookSkipRunStopped}, nil
		case err != nil:
			return stepOutcome{}, err
		case held:
			return stepOutcome{skipReason: types.PlaybookSkipReminderHeld}, nil
		}
		return stepOutcome{ref: reminder.ID}, nil
	}

	// Not overdue yet: the reminder queue only lists overdue invoices, so the email goes out as written. A step
	// that needs review (a step on the due date run before the invoice's due time) waits a day.
	if !step.SendWithoutReview {
		tomorrow := types.UTCDay(now).AddDate(0, 0, 1)
		return stepOutcome{deferTo: &tomorrow}, nil
	}
	if !s.Config.Email.IsProviderConfigured() {
		return stepOutcome{skipReason: types.PlaybookSkipEmailsUnavailable}, nil
	}
	recipients := c.EmailRecipients(types.EmailTypeInvoicePaymentReminder)
	if len(recipients) == 0 {
		return stepOutcome{skipReason: types.PlaybookSkipNoRecipient}, nil
	}
	e, err := NewEmailNotificationService(s.ServiceParams).QueueReminderEmail(ctx, ReminderEmail{
		InvoiceID: inv.ID, CustomerID: c.ID, ReminderID: sr.ID, Recipients: recipients,
		Subject: mail.subject, Body: mail.body, ApprovedAt: sr.DueAt,
	})
	if ierr.IsInvalidOperation(err) {
		return stepOutcome{skipReason: types.PlaybookSkipEmailsUnavailable}, nil
	}
	if err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{ref: e.ID}, nil
}

// emailGuardrail holds back a customer email that falls on a weekend (with skip_weekends) or within min_gap_days of
// the last one; it returns the day to try again and false.
func (s *playbookService) emailGuardrail(ctx context.Context, p *collections.Playbook, run *collections.PlaybookRun, now time.Time) (time.Time, bool) {
	today := types.UTCDay(now)
	if p.SkipWeekends {
		switch today.Weekday() {
		case time.Saturday:
			return today.AddDate(0, 0, 2), false
		case time.Sunday:
			return today.AddDate(0, 0, 1), false
		}
	}
	if p.MinGapDays > 0 {
		last, err := s.PlaybookRunRepo.LastCustomerEmailAt(ctx, run.CustomerID)
		if err == nil && last != nil {
			earliest := types.UTCDay(*last).AddDate(0, 0, p.MinGapDays)
			if today.Before(earliest) {
				return earliest, false
			}
		}
	}
	return today, true
}

func (s *playbookService) executeTaskStep(ctx context.Context, p *collections.Playbook, run *collections.PlaybookRun, sr *collections.PlaybookStepRun, step types.PlaybookStep, inv *invoice.Invoice, vars map[string]string) (stepOutcome, error) {
	note := fillPlaybookText(step.Note, vars)
	task := &collections.CollectionTask{
		ID: types.GenerateUUIDWithPrefix(types.UUID_PREFIX_COLLECTION_TASK), InvoiceID: run.InvoiceID, CustomerID: run.CustomerID,
		RunID: run.ID, StepID: step.ID, Note: note, State: types.CollectionTaskOpen,
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	recipient := ""
	if step.Assignee != nil {
		task.AssigneeUserID, task.AssigneeEmail = step.Assignee.UserID, step.Assignee.Email
		recipient = step.Assignee.Email
		if step.Assignee.UserID != "" {
			u, err := s.UserRepo.GetByID(ctx, step.Assignee.UserID)
			if err != nil && !ierr.IsNotFound(err) {
				return stepOutcome{}, err
			}
			if u != nil {
				recipient = u.Email
			}
		}
	}
	if err := s.PlaybookRunRepo.CreateTask(ctx, task); err != nil && !isAlreadyExists(err) {
		return stepOutcome{}, err
	}
	if recipient == "" || !s.Config.Email.IsProviderConfigured() {
		// The task is recorded either way; it is only not emailed.
		return stepOutcome{ref: task.ID}, nil
	}
	if _, err := s.queueNotice(ctx, p, run, sr, inv, []string{recipient}, "Task", note); err != nil {
		return stepOutcome{}, err
	}
	return stepOutcome{ref: task.ID}, nil
}

// queueNotice emails a team notice about the run's invoice.
func (s *playbookService) queueNotice(ctx context.Context, p *collections.Playbook, run *collections.PlaybookRun, sr *collections.PlaybookStepRun, inv *invoice.Invoice, to []string, label, body string) (stepOutcome, error) {
	if !s.Config.Email.IsProviderConfigured() {
		return stepOutcome{skipReason: types.PlaybookSkipEmailsUnavailable}, nil
	}
	number := lo.FromPtr(inv.InvoiceNumber)
	e, err := NewEmailNotificationService(s.ServiceParams).QueueTeamNotice(ctx, TeamNotice{
		InvoiceID: inv.ID, CustomerID: run.CustomerID, StepRunID: sr.ID, Recipients: to,
		Subject: fmt.Sprintf("[%s] %s: %s", label, p.Name, lo.CoalesceOrEmpty(number, inv.ID)),
		Heading: fmt.Sprintf("Collections playbook \"%s\"", p.Name),
		Body:    body,
	})
	if ierr.IsInvalidOperation(err) {
		return stepOutcome{skipReason: types.PlaybookSkipEmailsUnavailable}, nil
	}
	if err != nil {
		return stepOutcome{}, err
	}
	if e == nil {
		// The tenant has no reply-to address, so automatic emails are not sent.
		return stepOutcome{skipReason: types.PlaybookSkipEmailsUnavailable}, nil
	}
	return stepOutcome{ref: e.ID}, nil
}

// playbookVariables are the values a step's text is filled with.
func (s *playbookService) playbookVariables(ctx context.Context, inv *invoice.Invoice, c *customer.Customer, now time.Time) map[string]string {
	days := 0
	if inv.DueDate != nil {
		days = max(0, types.CalendarDaysBetween(*inv.DueDate, now))
	}
	due := ""
	if inv.DueDate != nil {
		due = inv.DueDate.UTC().Format("Jan 2, 2006")
	}
	link := ""
	if urls, err := NewHostedInvoiceTokenService(s.ServiceParams).HostedInvoiceURLs(ctx, []*invoice.Invoice{inv}); err != nil {
		// The email layout still links the invoice; only the inline link is left out.
		s.Logger.Info(ctx, "playbook: no hosted invoice link", "invoice_id", inv.ID, "reason", err.Error())
	} else {
		link = urls[inv.ID]
	}
	name := ""
	if c != nil {
		name = strings.TrimSpace(c.Name)
	}
	return map[string]string{
		types.PlaybookVarCustomer:    name,
		types.PlaybookVarInvoice:     lo.CoalesceOrEmpty(lo.FromPtr(inv.InvoiceNumber), inv.ID),
		types.PlaybookVarAmount:      reminderMoney(inv.AmountRemaining, inv.Currency),
		types.PlaybookVarDueDate:     due,
		types.PlaybookVarDaysOverdue: strconv.Itoa(days),
		types.PlaybookVarPayLink:     link,
	}
}

// fillPlaybookText replaces each playbook variable in text with its value.
func fillPlaybookText(text string, vars map[string]string) string {
	pairs := make([]string, 0, len(vars)*2)
	for _, token := range types.PlaybookVariables {
		pairs = append(pairs, token, vars[token])
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
