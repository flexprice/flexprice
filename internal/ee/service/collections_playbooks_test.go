package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

type playbookFixture struct {
	*reminderFixture
	store *testutil.InMemoryPlaybookStore
	users *testutil.InMemoryUserStore
	pb    PlaybookService
}

func newPlaybookFixture(t *testing.T, ctx context.Context) *playbookFixture {
	t.Helper()
	rf := newReminderFixture(t, ctx)
	f := &playbookFixture{reminderFixture: rf, store: testutil.NewInMemoryPlaybookStore(rf.invoices, rf.customers), users: testutil.NewInMemoryUserStore()}
	f.rebuild()
	return f
}

// rebuild makes the playbook service again from the fixture's current settings (call after withEmail).
func (f *playbookFixture) rebuild() {
	params := f.params(f.reminders)
	params.PlaybookRepo, params.PlaybookRunRepo, params.UserRepo = f.store, f.store, f.users
	f.pb = NewPlaybookService(params)
}

// steady turns the guardrails off so a test runs the same on any day of the week.
func steady(req *dto.UpdatePlaybookRequest) *dto.UpdatePlaybookRequest {
	req.MinGapDays, req.SkipWeekends = lo.ToPtr(0), lo.ToPtr(false)
	return req
}

// livePlaybook creates a playbook with the given audience and steps and activates it.
func (f *playbookFixture) livePlaybook(t *testing.T, ctx context.Context, name string, trigger types.PlaybookTrigger, audience types.PlaybookAudience, steps ...types.PlaybookStep) *dto.PlaybookResponse {
	t.Helper()
	created, err := f.pb.Create(ctx, &dto.CreatePlaybookRequest{Name: name})
	require.NoError(t, err)
	saved, err := f.pb.Update(ctx, created.ID, steady(&dto.UpdatePlaybookRequest{
		Version: created.Version, Name: name, Trigger: trigger, Audience: audience, Steps: steps,
		Stops: []types.PlaybookStopCondition{types.PlaybookStopManuallyPaused},
	}))
	require.NoError(t, err)
	live, err := f.pb.Activate(ctx, saved.ID, &dto.PlaybookVersionRequest{Version: saved.Version})
	require.NoError(t, err)
	return live
}

func (f *playbookFixture) run(t *testing.T, ctx context.Context) *dto.RunPlaybooksResponse {
	t.Helper()
	resp, err := f.pb.RunEnvironment(ctx)
	require.NoError(t, err)
	return resp
}

func (f *playbookFixture) reminderFor(t *testing.T, ctx context.Context, invoiceID string) *collections.InvoiceReminder {
	t.Helper()
	r, ok := lo.Find(f.reminders.All(ctx), func(r *collections.InvoiceReminder) bool { return r.InvoiceID == invoiceID })
	require.True(t, ok, "no reminder for %s", invoiceID)
	return r
}

func (f *playbookFixture) stepRun(t *testing.T, ctx context.Context, stepID string) *collections.PlaybookStepRun {
	t.Helper()
	sr, ok := lo.Find(f.store.StepRuns(ctx), func(s *collections.PlaybookStepRun) bool { return s.StepID == stepID })
	require.True(t, ok, "no step run for %s", stepID)
	return sr
}

func pbEmail(id string, day int, tone types.ReminderTone, auto bool) types.PlaybookStep {
	return types.PlaybookStep{ID: id, Kind: types.PlaybookStepEmail, Day: day, Tone: tone, Subject: "{{invoice}} for {{customer}}", Body: "Hi {{customer}}, {{invoice}} for {{amount}} was due {{due_date}} ({{days_overdue}} days).", SendWithoutReview: auto}
}

var everyone = types.PlaybookAudience{Kind: types.PlaybookAudienceAll}

func TestPlaybook_CreateBlankAndFromTemplate(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)

	blank, err := f.pb.Create(ctx, &dto.CreatePlaybookRequest{})
	require.NoError(t, err)
	require.Equal(t, "Untitled playbook", blank.Name)
	require.Equal(t, types.PlaybookStateDraft, blank.State)
	require.Equal(t, 1, blank.Priority)
	require.Equal(t, 1, blank.Version)
	require.Len(t, blank.Steps, 1)
	require.Equal(t, []types.PlaybookStopCondition{types.PlaybookStopInvoicePaid, types.PlaybookStopManuallyPaused}, blank.Stops)
	require.Empty(t, blank.Problems, "the blank playbook is ready to go live")

	ent, err := f.pb.Create(ctx, &dto.CreatePlaybookRequest{TemplateID: "enterprise"})
	require.NoError(t, err)
	require.Equal(t, "Enterprise, account-managed", ent.Name)
	require.Equal(t, 2, ent.Priority)
	require.Equal(t, "enterprise", ent.TemplateID)
	require.Equal(t, types.PlaybookAudience{Kind: types.PlaybookAudienceSegment, Key: "segment", Value: "enterprise"}, ent.Audience)
	require.Equal(t, []int{7, 14, 21, 35}, lo.Map(ent.Steps, func(s types.PlaybookStep, _ int) int { return s.Day }))
	require.ElementsMatch(t, []string{"to", "assignee"}, lo.Map(ent.Problems, func(p types.PlaybookProblem, _ int) string { return p.Field }),
		"the template's alert and task need a recipient before it can go live")

	_, err = f.pb.Create(ctx, &dto.CreatePlaybookRequest{TemplateID: "nope"})
	requireKind(t, err, ierr.ErrValidation)

	templates, err := f.pb.ListTemplates(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"standard", "before-due", "enterprise", "high-value", "final-push"},
		lo.Map(templates.Items, func(t dto.PlaybookTemplateResponse, _ int) string { return t.ID }))
}

func TestPlaybook_UpdateRefusesStaleVersionAndKeepsLivePlaybooksReady(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	p, err := f.pb.Create(ctx, &dto.CreatePlaybookRequest{Name: "Chase"})
	require.NoError(t, err)

	draft := steady(&dto.UpdatePlaybookRequest{Version: p.Version, Name: "Chase", Trigger: types.PlaybookTriggerOverdue,
		Steps: []types.PlaybookStep{{ID: "s1", Kind: types.PlaybookStepEmail, Day: 3, Tone: types.ReminderToneFriendly}}})
	saved, err := f.pb.Update(ctx, p.ID, draft)
	require.NoError(t, err, "a draft may be incomplete")
	require.Equal(t, 2, saved.Version)
	require.Len(t, saved.Problems, 2)

	_, err = f.pb.Update(ctx, p.ID, draft)
	requireKind(t, err, ierr.ErrVersionConflict)

	_, err = f.pb.Activate(ctx, p.ID, &dto.PlaybookVersionRequest{Version: saved.Version})
	requireKind(t, err, ierr.ErrInvalidOperation)
	require.Len(t, playbookErrorDetails(err)["problems"], 2, "activation lists every problem, as the API returns it in details")

	draft.Version, draft.Steps = saved.Version, []types.PlaybookStep{pbEmail("s1", 3, types.ReminderToneFriendly, false)}
	saved, err = f.pb.Update(ctx, p.ID, draft)
	require.NoError(t, err)
	live, err := f.pb.Activate(ctx, p.ID, &dto.PlaybookVersionRequest{Version: saved.Version})
	require.NoError(t, err)
	require.Equal(t, types.PlaybookStateLive, live.State)
	require.NotNil(t, live.ActivatedAt)

	draft.Version, draft.Steps = live.Version, []types.PlaybookStep{{ID: "s1", Kind: types.PlaybookStepEmail, Day: 3, Tone: types.ReminderToneFriendly}}
	_, err = f.pb.Update(ctx, p.ID, draft)
	requireKind(t, err, ierr.ErrInvalidOperation)

	_, err = f.pb.Activate(ctx, p.ID, &dto.PlaybookVersionRequest{Version: live.Version})
	requireKind(t, err, ierr.ErrVersionConflict)
}

func TestPlaybook_UpdateRequestValidation(t *testing.T) {
	valid := func() *dto.UpdatePlaybookRequest {
		return &dto.UpdatePlaybookRequest{Version: 1, Name: "x", Trigger: types.PlaybookTriggerOverdue, Steps: []types.PlaybookStep{pbEmail("a", 3, types.ReminderToneFriendly, false)}}
	}
	cases := map[string]func(r *dto.UpdatePlaybookRequest){
		"no version":       func(r *dto.UpdatePlaybookRequest) { r.Version = 0 },
		"no trigger":       func(r *dto.UpdatePlaybookRequest) { r.Trigger = "" },
		"bad trigger":      func(r *dto.UpdatePlaybookRequest) { r.Trigger = "later" },
		"day too late":     func(r *dto.UpdatePlaybookRequest) { r.Steps[0].Day = 181 },
		"day too early":    func(r *dto.UpdatePlaybookRequest) { r.Steps[0].Day = -31 },
		"bad tone":         func(r *dto.UpdatePlaybookRequest) { r.Steps[0].Tone = "angry" },
		"unknown variable": func(r *dto.UpdatePlaybookRequest) { r.Steps[0].Body = "Hi {{first_name}}" },
		"duplicate ids": func(r *dto.UpdatePlaybookRequest) {
			r.Steps = append(r.Steps, pbEmail("a", 9, types.ReminderToneFirm, false))
		},
		"bad step id":  func(r *dto.UpdatePlaybookRequest) { r.Steps[0].ID = "has space" },
		"unknown kind": func(r *dto.UpdatePlaybookRequest) { r.Steps[0].Kind = "sms" },
		"bad stop":     func(r *dto.UpdatePlaybookRequest) { r.Stops = []types.PlaybookStopCondition{"moon"} },
		"gap too long": func(r *dto.UpdatePlaybookRequest) { r.MinGapDays = lo.ToPtr(31) },
		"segment no value": func(r *dto.UpdatePlaybookRequest) {
			r.Audience = types.PlaybookAudience{Kind: types.PlaybookAudienceSegment}
		},
		"amount no currency": func(r *dto.UpdatePlaybookRequest) {
			r.Audience = types.PlaybookAudience{Kind: types.PlaybookAudienceAmountOver, Amount: lo.ToPtr(decimal.NewFromInt(5))}
		},
		"zero amount": func(r *dto.UpdatePlaybookRequest) {
			r.Audience = types.PlaybookAudience{Kind: types.PlaybookAudienceAmountOver, Amount: lo.ToPtr(decimal.Zero), Currency: "usd"}
		},
		"days zero": func(r *dto.UpdatePlaybookRequest) {
			r.Audience = types.PlaybookAudience{Kind: types.PlaybookAudienceDaysOverdueOver}
		},
		"too many":     func(r *dto.UpdatePlaybookRequest) { r.Steps = make([]types.PlaybookStep, 21) },
		"long subject": func(r *dto.UpdatePlaybookRequest) { r.Steps[0].Subject = string(make([]byte, 256)) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r := valid()
			mutate(r)
			requireKind(t, r.Validate(), ierr.ErrValidation)
		})
	}

	r := valid()
	r.Audience = types.PlaybookAudience{Kind: types.PlaybookAudienceAmountOver, Amount: lo.ToPtr(decimal.NewFromInt(10)), Currency: " USD ", Days: 4, Value: "x"}
	r.Stops = []types.PlaybookStopCondition{types.PlaybookStopDisputeFiled, types.PlaybookStopDisputeFiled}
	require.NoError(t, r.Validate())
	require.Equal(t, types.PlaybookAudience{Kind: types.PlaybookAudienceAmountOver, Amount: lo.ToPtr(decimal.NewFromInt(10)), Currency: "usd"}, r.Audience, "fields of other kinds are dropped")
	require.Equal(t, []types.PlaybookStopCondition{types.PlaybookStopInvoicePaid, types.PlaybookStopDisputeFiled}, r.Stops, "invoice_paid is always on")
}

func TestPlaybook_Readiness(t *testing.T) {
	p := &collections.Playbook{Name: "x", Trigger: types.PlaybookTriggerBeforeDue, Steps: []types.PlaybookStep{
		pbEmail("late", 4, types.ReminderToneFriendly, false),
		{ID: "early", Kind: types.PlaybookStepEmail, Day: -2, Tone: types.ReminderToneFriendly, Subject: "s", Body: "b"},
		{ID: "alert", Kind: types.PlaybookStepInternal, Day: 5, Channel: types.PlaybookChannelEmail, To: "not-an-email", Note: "n"},
		{ID: "task", Kind: types.PlaybookStepTask, Day: 6, Note: "call", Assignee: &types.PlaybookTaskAssignee{Email: "ops@flex.test"}},
	}}
	require.Equal(t, []types.PlaybookProblem{
		{StepID: "early", Field: "send_without_review", Message: "A reminder before the due date is sent without review; turn on send without review"},
		{StepID: "alert", Field: "to", Message: "Enter a valid email address"},
	}, p.Problems())

	p = &collections.Playbook{Trigger: types.PlaybookTriggerBeforeDue, Steps: []types.PlaybookStep{{ID: "t", Kind: types.PlaybookStepTask, Day: 3}}}
	require.ElementsMatch(t, []string{"name", "assignee", "note", "steps", "trigger"}, lo.Map(p.Problems(), func(p types.PlaybookProblem, _ int) string { return p.Field }))
}

func TestPlaybook_ReorderAndDeleteRenumber(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	a, _ := f.pb.Create(ctx, &dto.CreatePlaybookRequest{Name: "A"})
	b, _ := f.pb.Create(ctx, &dto.CreatePlaybookRequest{Name: "B"})
	c, _ := f.pb.Create(ctx, &dto.CreatePlaybookRequest{Name: "C"})

	_, err := f.pb.Reorder(ctx, &dto.ReorderPlaybooksRequest{IDs: []string{c.ID, a.ID}})
	requireKind(t, err, ierr.ErrValidation)
	_, err = f.pb.Reorder(ctx, &dto.ReorderPlaybooksRequest{IDs: []string{c.ID, a.ID, a.ID}})
	requireKind(t, err, ierr.ErrValidation)

	list, err := f.pb.Reorder(ctx, &dto.ReorderPlaybooksRequest{IDs: []string{c.ID, a.ID, b.ID}})
	require.NoError(t, err)
	require.Equal(t, []string{"C", "A", "B"}, lo.Map(list.Items, func(p *dto.PlaybookResponse, _ int) string { return p.Name }))
	require.Equal(t, []int{1, 2, 3}, lo.Map(list.Items, func(p *dto.PlaybookResponse, _ int) int { return p.Priority }))

	require.NoError(t, f.pb.Delete(ctx, a.ID))
	list, err = f.pb.List(ctx, &dto.ListPlaybooksRequest{})
	require.NoError(t, err)
	require.Equal(t, []string{"C", "B"}, lo.Map(list.Items, func(p *dto.PlaybookResponse, _ int) string { return p.Name }))
	require.Equal(t, []int{1, 2}, lo.Map(list.Items, func(p *dto.PlaybookResponse, _ int) int { return p.Priority }))

	dup, err := f.pb.Duplicate(ctx, c.ID)
	require.NoError(t, err)
	require.Equal(t, "C (copy)", dup.Name)
	require.Equal(t, 3, dup.Priority)
	require.Equal(t, types.PlaybookStateDraft, dup.State)

	_, err = f.pb.Get(ctx, a.ID)
	requireKind(t, err, ierr.ErrNotFound)
}

func TestPlaybook_EngineEnrolsOnlyFutureStepsAndWritesTheReminder(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme Robotics", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 4200, 5)
	p := f.livePlaybook(t, ctx, "Standard", types.PlaybookTriggerOverdue, everyone,
		pbEmail("past", 3, types.ReminderToneFriendly, false), pbEmail("today", 5, types.ReminderToneFirm, false), pbEmail("later", 10, types.ReminderToneFinal, false))

	resp := f.run(t, ctx)
	require.Equal(t, 1, resp.RunsEnrolled)
	require.Equal(t, 1, resp.StepsExecuted)

	steps := f.store.StepRuns(ctx)
	require.Equal(t, []string{"today", "later"}, lo.Map(steps, func(s *collections.PlaybookStepRun, _ int) string { return s.StepID }),
		"a step already past when the invoice enrols is never scheduled")
	today := f.stepRun(t, ctx, "today")
	require.Equal(t, types.PlaybookStepRunDone, today.State)

	r := f.reminderFor(t, ctx, "a1")
	require.Equal(t, today.ResultRef, r.ID)
	require.Equal(t, types.ReminderStatusPending, r.ReminderStatus, "the reminder waits for review")
	require.Equal(t, types.ReminderToneFirm, r.Tone)
	require.Equal(t, "INV-a1 for Acme Robotics", r.Subject)
	require.Contains(t, r.Body, "Hi Acme Robotics, INV-a1 for $4,200 was due")
	require.Contains(t, r.Body, "(5 days)")
	require.Equal(t, p.ID, r.PlaybookID)
	require.Equal(t, "today", r.PlaybookStepID)

	view, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, "INV-a1 for Acme Robotics", view.Subject, "the queue shows the step's text, not the age-based draft")
	require.Equal(t, "today", view.PlaybookStepID)
	require.False(t, view.Edited)
	require.Equal(t, "Tone set to firm by the playbook step (invoice is 5 days overdue).", view.ToneReason)

	approved, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Equal(t, "INV-a1 for Acme Robotics", approved.Subject, "approving keeps the step's text")
	require.Equal(t, "INV-a1 for Acme Robotics", f.emails(t, ctx, "a1")[0].Subject)

	again := f.run(t, ctx)
	require.Zero(t, again.RunsEnrolled)
	require.Zero(t, again.StepsExecuted, "a step runs once per invoice")

	runs, err := f.pb.ListRuns(ctx, p.ID, &dto.ListPlaybookRunsRequest{})
	require.NoError(t, err)
	require.Len(t, runs.Items, 1)
	require.Equal(t, "INV-a1", runs.Items[0].InvoiceNumber)
	require.Equal(t, "Acme Robotics", runs.Items[0].CustomerName)
	require.Equal(t, "later", runs.Items[0].NextStep.StepID)
	require.Equal(t, 1, runs.Pagination.Total)
}

func TestPlaybook_SendWithoutReviewApprovesAndQueuesTheEmail(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 2)
	f.livePlaybook(t, ctx, "Auto", types.PlaybookTriggerOverdue, everyone, pbEmail("auto", 2, types.ReminderToneFriendly, true))

	require.Equal(t, 1, f.run(t, ctx).StepsExecuted)
	r := f.reminderFor(t, ctx, "a1")
	require.Equal(t, types.ReminderStatusApproved, r.ReminderStatus)
	require.NotNil(t, r.ApprovedAt)
	logs := f.emails(t, ctx, "a1")
	require.Len(t, logs, 1)
	require.Equal(t, types.EmailTemplateInvoicePaymentReminder, logs[0].TemplateKey)
	require.Equal(t, "INV-a1 for Acme", logs[0].Subject)
	require.Equal(t, []string{"ap@acme.test"}, logs[0].ToAddresses)

	// Without a recipient the reminder waits for review instead.
	f.customer(t, ctx, "cust_2", "Nomail", "")
	f.overdue(t, ctx, "a2", "cust_2", 100, 2)
	f.run(t, ctx)
	require.Equal(t, types.ReminderStatusPending, f.reminderFor(t, ctx, "a2").ReminderStatus)
	require.Empty(t, f.emails(t, ctx, "a2"))
}

func TestPlaybook_HeldReminderStopsEmailSteps(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 4)
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusHeld, "Disputed line item"))
	require.NoError(t, err)
	f.livePlaybook(t, ctx, "Std", types.PlaybookTriggerOverdue, everyone, pbEmail("e", 4, types.ReminderToneFirm, false))

	resp := f.run(t, ctx)
	require.Equal(t, 1, resp.StepsSkipped)
	sr := f.stepRun(t, ctx, "e")
	require.Equal(t, types.PlaybookStepRunSkipped, sr.State)
	require.Equal(t, types.PlaybookSkipReminderHeld, sr.SkipReason)
	r := f.reminderFor(t, ctx, "a1")
	require.Equal(t, types.ReminderStatusHeld, r.ReminderStatus, "a person's hold is never overwritten")
	require.Empty(t, r.PlaybookStepID)
}

func TestPlaybook_PaidInvoiceStopsItsRun(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 1)
	f.livePlaybook(t, ctx, "Std", types.PlaybookTriggerOverdue, everyone, pbEmail("e1", 1, types.ReminderToneFriendly, false), pbEmail("e2", 8, types.ReminderToneFirm, false))
	f.run(t, ctx)

	f.pay(t, ctx, "a1")
	resp := f.run(t, ctx)
	require.Equal(t, 1, resp.RunsStopped)
	run := f.store.Runs(ctx)[0]
	require.Equal(t, types.PlaybookRunStopped, run.State)
	require.Equal(t, types.PlaybookStopReasonPaid, run.StopReason)
	require.Equal(t, types.PlaybookSkipRunStopped, f.stepRun(t, ctx, "e2").SkipReason)
	require.Zero(t, f.run(t, ctx).RunsEnrolled, "a paid invoice is not enrolled again")
}

func TestPlaybook_FirstLivePlaybookByPriorityWins(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.customer(t, ctx, "cust_2", "Bolt", "ap@bolt.test")
	require.NoError(t, f.customers.Update(ctx, &customer.Customer{ID: "cust_2", Name: "Bolt", Email: "ap@bolt.test", Metadata: map[string]string{"segment": "Enterprise"}, BaseModel: types.GetDefaultBaseModel(ctx)}))
	f.overdue(t, ctx, "big", "cust_1", 50000, 3)
	f.overdue(t, ctx, "small", "cust_1", 100, 3)
	f.overdue(t, ctx, "ent", "cust_2", 200, 3)
	f.overdue(t, ctx, "eur", "cust_1", 90000, 3)
	inv := lo.Must(f.invoices.Get(ctx, "eur"))
	inv.Currency = "eur"
	require.NoError(t, f.invoices.Update(ctx, inv))

	high := f.livePlaybook(t, ctx, "High value", types.PlaybookTriggerOverdue,
		types.PlaybookAudience{Kind: types.PlaybookAudienceAmountOver, Amount: lo.ToPtr(decimal.NewFromInt(10000)), Currency: "usd"}, pbEmail("h", 3, types.ReminderToneFirm, false))
	ent := f.livePlaybook(t, ctx, "Enterprise", types.PlaybookTriggerOverdue,
		types.PlaybookAudience{Kind: types.PlaybookAudienceSegment, Key: "segment", Value: "enterprise"}, pbEmail("x", 3, types.ReminderToneFriendly, false))
	all := f.livePlaybook(t, ctx, "Everyone", types.PlaybookTriggerOverdue, everyone, pbEmail("a", 3, types.ReminderToneFriendly, false))

	preview, err := f.pb.Preview(ctx, all.ID)
	require.NoError(t, err)
	require.Equal(t, 2, preview.MatchingCount, "small and eur go to everyone")
	require.Equal(t, 2, preview.TakenByHigherPriority, "big and ent are taken first")

	require.Equal(t, 4, f.run(t, ctx).RunsEnrolled)
	byInvoice := lo.SliceToMap(f.store.Runs(ctx), func(r *collections.PlaybookRun) (string, string) { return r.InvoiceID, r.PlaybookID })
	require.Equal(t, map[string]string{"big": high.ID, "ent": ent.ID, "small": all.ID, "eur": all.ID}, byInvoice)
}

func TestPlaybook_DeleteReleasesInvoicesToTheNextPlaybook(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 2)
	first := f.livePlaybook(t, ctx, "First", types.PlaybookTriggerOverdue, everyone, pbEmail("f", 9, types.ReminderToneFriendly, false))
	second := f.livePlaybook(t, ctx, "Second", types.PlaybookTriggerOverdue, everyone, pbEmail("s", 2, types.ReminderToneFirm, false))
	f.run(t, ctx)
	require.Equal(t, first.ID, f.store.Runs(ctx)[0].PlaybookID)

	require.NoError(t, f.pb.Delete(ctx, first.ID))
	resp := f.run(t, ctx)
	require.Equal(t, 1, resp.RunsEnrolled)
	runs := lo.SliceToMap(f.store.Runs(ctx), func(r *collections.PlaybookRun) (string, *collections.PlaybookRun) { return r.PlaybookID, r })
	require.Equal(t, types.PlaybookStopReasonPlaybookDeleted, runs[first.ID].StopReason)
	require.Equal(t, types.PlaybookSkipRunStopped, f.stepRun(t, ctx, "f").SkipReason)
	require.Equal(t, types.PlaybookRunCompleted, runs[second.ID].State, "its only step ran, so the run is complete")
	require.Equal(t, types.PlaybookStepRunDone, f.stepRun(t, ctx, "s").State)
}

func TestPlaybook_PausedPlaybookAndRunRunNothing(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 1)
	f.overdue(t, ctx, "a2", "cust_1", 100, 1)
	p := f.livePlaybook(t, ctx, "Std", types.PlaybookTriggerOverdue, everyone, pbEmail("e", 3, types.ReminderToneFriendly, false))
	require.Equal(t, 2, f.run(t, ctx).RunsEnrolled)

	runA1, _ := lo.Find(f.store.Runs(ctx), func(r *collections.PlaybookRun) bool { return r.InvoiceID == "a1" })
	paused, err := f.pb.PauseRun(ctx, runA1.ID, &dto.PausePlaybookRunRequest{Reason: "Talking to their CFO"})
	require.NoError(t, err)
	require.Equal(t, types.PlaybookRunPaused, paused.State)
	_, err = f.pb.PauseRun(ctx, runA1.ID, &dto.PausePlaybookRunRequest{})
	requireKind(t, err, ierr.ErrVersionConflict)

	cust, err := f.pb.PauseCustomer(ctx, "cust_1", &dto.PausePlaybookRunRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, cust.RunsAffected, "only a2 was still active")

	resumed, err := f.pb.ResumeCustomer(ctx, "cust_1")
	require.NoError(t, err)
	require.Equal(t, 2, resumed.RunsAffected)

	pausedPlaybook, err := f.pb.Pause(ctx, p.ID, &dto.PlaybookVersionRequest{Version: p.Version})
	require.NoError(t, err)
	require.Equal(t, types.PlaybookStatePaused, pausedPlaybook.State)
	f.overdue(t, ctx, "a3", "cust_1", 100, 1)
	require.Zero(t, f.run(t, ctx).RunsEnrolled, "a paused playbook enrols nothing")

	_, err = f.pb.PauseCustomer(ctx, "nope", &dto.PausePlaybookRunRequest{})
	requireKind(t, err, ierr.ErrNotFound)
}

func TestPlaybook_PauseNeedsTheManualStop(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 1)
	created, _ := f.pb.Create(ctx, &dto.CreatePlaybookRequest{Name: "No pause"})
	saved, err := f.pb.Update(ctx, created.ID, steady(&dto.UpdatePlaybookRequest{Version: 1, Name: "No pause", Trigger: types.PlaybookTriggerOverdue,
		Steps: []types.PlaybookStep{pbEmail("e", 5, types.ReminderToneFriendly, false)}}))
	require.NoError(t, err)
	_, err = f.pb.Activate(ctx, saved.ID, &dto.PlaybookVersionRequest{Version: saved.Version})
	require.NoError(t, err)
	f.run(t, ctx)

	_, err = f.pb.PauseRun(ctx, f.store.Runs(ctx)[0].ID, &dto.PausePlaybookRunRequest{})
	requireKind(t, err, ierr.ErrInvalidOperation)
}

func TestPlaybook_SupersededSteps(t *testing.T) {
	day0 := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	now := day0.AddDate(0, 0, 10).Add(9 * time.Hour)
	steps := []*collections.PlaybookStepRun{
		{ID: "1", RunID: "r", DueAt: day0, State: types.PlaybookStepRunPending},
		{ID: "2", RunID: "r", DueAt: day0.AddDate(0, 0, 7), State: types.PlaybookStepRunPending},
		{ID: "3", RunID: "r", DueAt: day0.AddDate(0, 0, 7), State: types.PlaybookStepRunPending},
		{ID: "4", RunID: "r", DueAt: day0.AddDate(0, 0, 14), State: types.PlaybookStepRunPending},
		{ID: "5", RunID: "r", DueAt: day0.AddDate(0, 0, 1), State: types.PlaybookStepRunDone},
		{ID: "6", RunID: "other", DueAt: day0, State: types.PlaybookStepRunPending},
	}
	require.Equal(t, []string{"1"}, lo.Map(supersededSteps(steps, now), func(s *collections.PlaybookStepRun, _ int) string { return s.ID }),
		"only steps older than each run's latest due day are skipped; steps sharing that day all run")
}

func TestPlaybook_EmailGuardrails(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	svc := f.pb.(*playbookService)
	run := &collections.PlaybookRun{ID: "r", CustomerID: "cust_1"}
	saturday := time.Date(2026, 11, 28, 9, 0, 0, 0, time.UTC)

	day, ok := svc.emailGuardrail(ctx, &collections.Playbook{SkipWeekends: true}, run, saturday)
	require.False(t, ok)
	require.Equal(t, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), day, "a weekend email waits for Monday")
	day, ok = svc.emailGuardrail(ctx, &collections.Playbook{SkipWeekends: true}, run, saturday.AddDate(0, 0, 1))
	require.False(t, ok)
	require.Equal(t, time.Date(2026, 11, 30, 0, 0, 0, 0, time.UTC), day)
	_, ok = svc.emailGuardrail(ctx, &collections.Playbook{SkipWeekends: false}, run, saturday)
	require.True(t, ok)

	// A customer emailed by a playbook two days ago waits for the gap.
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 3)
	f.livePlaybook(t, ctx, "Std", types.PlaybookTriggerOverdue, everyone, pbEmail("e", 3, types.ReminderToneFriendly, false))
	f.run(t, ctx)
	now := clock.Now()
	day, ok = svc.emailGuardrail(ctx, &collections.Playbook{MinGapDays: 3}, run, now)
	require.False(t, ok)
	require.Equal(t, types.UTCDay(now).AddDate(0, 0, 3), day)
	_, ok = svc.emailGuardrail(ctx, &collections.Playbook{MinGapDays: 3}, run, now.AddDate(0, 0, 3))
	require.True(t, ok)
}

func TestPlaybook_InternalAlertAndTaskSteps(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 7000, 6)
	f.livePlaybook(t, ctx, "Escalate", types.PlaybookTriggerOverdue, everyone,
		pbEmail("e", 20, types.ReminderToneFriendly, false),
		types.PlaybookStep{ID: "alert", Kind: types.PlaybookStepInternal, Day: 6, Channel: types.PlaybookChannelEmail, To: "finance@flex.test", Note: "{{invoice}} ({{amount}}) is {{days_overdue}} days overdue."},
		types.PlaybookStep{ID: "slack", Kind: types.PlaybookStepInternal, Day: 6, Channel: types.PlaybookChannelSlack, To: "#ar", Note: "x"},
		types.PlaybookStep{ID: "call", Kind: types.PlaybookStepTask, Day: 6, Assignee: &types.PlaybookTaskAssignee{Email: "owner@flex.test"}, Note: "Call {{customer}}."},
	)

	resp := f.run(t, ctx)
	require.Equal(t, 2, resp.StepsExecuted)
	require.Equal(t, 1, resp.StepsSkipped)
	require.Equal(t, types.PlaybookSkipSlackNotConnected, f.stepRun(t, ctx, "slack").SkipReason)

	notices := lo.Filter(f.emails(t, ctx, "a1"), func(e *emaillog.EmailLog, _ int) bool {
		return e.TemplateKey == types.EmailTemplateCollectionsTeamNotice
	})
	require.Len(t, notices, 2)
	alert, _ := lo.Find(notices, func(e *emaillog.EmailLog) bool { return e.ToAddresses[0] == "finance@flex.test" })
	require.Equal(t, "[Alert] Escalate: INV-a1", alert.Subject)
	require.Equal(t, "INV-a1 ($7,000) is 6 days overdue.", alert.Metadata[emailMetaNoticeBody])
	require.Equal(t, alert.ID, f.stepRun(t, ctx, "alert").ResultRef)

	tasks, err := f.pb.ListTasks(ctx, &dto.ListCollectionTasksRequest{InvoiceID: "a1"})
	require.NoError(t, err)
	require.Len(t, tasks.Items, 1)
	task := tasks.Items[0]
	require.Equal(t, "Call Acme.", task.Note)
	require.Equal(t, "owner@flex.test", task.AssigneeEmail)
	require.Equal(t, types.CollectionTaskOpen, task.State)
	require.Equal(t, task.ID, f.stepRun(t, ctx, "call").ResultRef)

	done, err := f.pb.CompleteTask(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, types.CollectionTaskDone, done.State)
	require.NotNil(t, done.DoneAt)
	_, err = f.pb.CompleteTask(ctx, task.ID)
	requireKind(t, err, ierr.ErrVersionConflict)
	reopened, err := f.pb.ReopenTask(ctx, task.ID)
	require.NoError(t, err)
	require.Nil(t, reopened.DoneAt)
}

func TestPlaybook_BeforeDueStepSendsWithoutAReminder(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	due := types.UTCDay(clock.Now()).AddDate(0, 0, 3)
	f.invoice(t, ctx, "n1", func(i *invoice.Invoice) { i.CustomerID, i.DueDate = "cust_1", &due })
	f.livePlaybook(t, ctx, "Heads-up", types.PlaybookTriggerBeforeDue, everyone, pbEmail("early", -3, types.ReminderToneFriendly, true), pbEmail("after", 2, types.ReminderToneFriendly, false))

	resp := f.run(t, ctx)
	require.Equal(t, 1, resp.RunsEnrolled)
	require.Equal(t, 1, resp.StepsExecuted)
	require.Empty(t, f.reminders.All(ctx), "the review queue only holds overdue invoices")
	logs := f.emails(t, ctx, "n1")
	require.Len(t, logs, 1)
	require.Equal(t, logs[0].ID, f.stepRun(t, ctx, "early").ResultRef)
	require.Contains(t, logs[0].Metadata[emailMetaReminderBody], "(0 days)")

	// An invoice not yet in its window is left for a later day.
	later := types.UTCDay(clock.Now()).AddDate(0, 0, 10)
	f.invoice(t, ctx, "n2", func(i *invoice.Invoice) { i.CustomerID, i.DueDate = "cust_1", &later })
	require.Zero(t, f.run(t, ctx).RunsEnrolled)
}

func TestPlaybook_LiveEditCarriesIntoPendingSteps(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 2)
	p := f.livePlaybook(t, ctx, "Std", types.PlaybookTriggerOverdue, everyone,
		pbEmail("keep", 10, types.ReminderToneFirm, false), pbEmail("drop", 12, types.ReminderToneFinal, false), pbEmail("turned", 11, types.ReminderToneFirm, false))
	f.run(t, ctx)

	_, err := f.pb.Update(ctx, p.ID, steady(&dto.UpdatePlaybookRequest{Version: p.Version, Name: "Std", Trigger: types.PlaybookTriggerOverdue,
		Steps: []types.PlaybookStep{pbEmail("keep", 15, types.ReminderToneFirm, false), pbEmail("new", 20, types.ReminderToneFinal, false), pbEmail("gone-by", 1, types.ReminderToneFriendly, false),
			{ID: "turned", Kind: types.PlaybookStepTask, Day: 11, Assignee: &types.PlaybookTaskAssignee{Email: "ops@flex.test"}, Note: "Call"}},
		Stops: []types.PlaybookStopCondition{types.PlaybookStopManuallyPaused}}))
	require.NoError(t, err)

	due := types.UTCDay(clock.Now()).AddDate(0, 0, -2)
	require.Equal(t, due.AddDate(0, 0, 15), f.stepRun(t, ctx, "keep").DueAt, "a moved step is rescheduled")
	require.Equal(t, types.PlaybookSkipEditedOut, f.stepRun(t, ctx, "drop").SkipReason)
	require.Equal(t, due.AddDate(0, 0, 20), f.stepRun(t, ctx, "new").DueAt)
	turned := f.stepRun(t, ctx, "turned")
	require.Equal(t, types.PlaybookStepTask, turned.Kind, "a step that changed kind keeps its row and runs as its new kind")
	require.Equal(t, types.PlaybookStepRunPending, turned.State)
	_, scheduled := lo.Find(f.store.StepRuns(ctx), func(s *collections.PlaybookStepRun) bool { return s.StepID == "gone-by" })
	require.False(t, scheduled, "a new step whose day already passed is not sent late")
}

func TestPlaybook_StatsCountRemindersAndRecovery(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 300, 2)
	f.overdue(t, ctx, "a2", "cust_1", 500, 2)
	p := f.livePlaybook(t, ctx, "Std", types.PlaybookTriggerOverdue, everyone, pbEmail("e", 2, types.ReminderToneFriendly, true), pbEmail("later", 30, types.ReminderToneFirm, false))
	f.run(t, ctx)

	inv := lo.Must(f.invoices.Get(ctx, "a1"))
	inv.PaymentStatus, inv.AmountPaid, inv.AmountRemaining, inv.PaidAt = types.PaymentStatusSucceeded, decimal.NewFromInt(300), decimal.Zero, lo.ToPtr(clock.Now())
	require.NoError(t, f.invoices.Update(ctx, inv))
	f.run(t, ctx)

	got, err := f.pb.Get(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, 1, got.Stats.InProgress)
	require.Equal(t, 2, got.Stats.RemindersSent30d)
	require.Equal(t, []dto.CollectionsCurrencyTotal{{Currency: "usd", Amount: decimal.NewFromInt(300)}}, got.Stats.Recovered30d)
	require.InDelta(t, 0.5, *got.Stats.PaidWithin7dRate, 1e-9)
}

func TestPlaybook_EnvironmentsAreIsolated(t *testing.T) {
	ctxA := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctxA)
	f.customer(t, ctxA, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctxA, "a1", "cust_1", 100, 3)
	p := f.livePlaybook(t, ctxA, "Std", types.PlaybookTriggerOverdue, everyone, pbEmail("e", 3, types.ReminderToneFriendly, false))

	ctxB := reminderCtxFor("tenant_a", "env_b")
	f.setCollections(t, ctxB, true)
	_, err := f.pb.Get(ctxB, p.ID)
	requireKind(t, err, ierr.ErrNotFound)
	list, err := f.pb.List(ctxB, &dto.ListPlaybooksRequest{})
	require.NoError(t, err)
	require.Empty(t, list.Items)
	require.Zero(t, f.run(t, ctxB).RunsEnrolled)

	ctxOther := reminderCtxFor("tenant_b", "env_a")
	f.setCollections(t, ctxOther, true)
	_, err = f.pb.Get(ctxOther, p.ID)
	requireKind(t, err, ierr.ErrNotFound)

	require.NoError(t, f.pb.RunPlaybooks(context.Background()), "the scheduler needs no tenant on its context")
	require.Len(t, f.store.Runs(ctxA), 1)
	require.Empty(t, f.store.Runs(ctxB))
}

func TestPlaybook_CollectionsMustBeOn(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newPlaybookFixture(t, ctx)
	f.setCollections(t, ctx, false)
	_, err := f.pb.List(ctx, &dto.ListPlaybooksRequest{})
	requireKind(t, err, ierr.ErrPermissionDenied)
	_, err = f.pb.Create(ctx, &dto.CreatePlaybookRequest{})
	requireKind(t, err, ierr.ErrPermissionDenied)
	_, err = f.pb.RunEnvironment(ctx)
	requireKind(t, err, ierr.ErrPermissionDenied)
}

// playbookErrorDetails reads the details an error carries, as the error middleware renders them.
func playbookErrorDetails(err error) map[string]any {
	out := map[string]any{}
	for _, sdp := range errors.GetAllSafeDetails(err) {
		for _, payload := range sdp.SafeDetails {
			if raw, ok := strings.CutPrefix(payload, "__json__:"); ok {
				_ = json.Unmarshal([]byte(raw), &out)
			}
		}
	}
	return out
}
