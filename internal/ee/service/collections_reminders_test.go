package service

import (
	"context"
	"fmt"
	"github.com/cockroachdb/errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

func TestReminderToneForDays(t *testing.T) {
	cases := map[int]types.ReminderTone{
		0: types.ReminderToneFriendly, 1: types.ReminderToneFriendly, 30: types.ReminderToneFriendly,
		31: types.ReminderToneFirm, 60: types.ReminderToneFirm,
		61: types.ReminderToneFinal, 200: types.ReminderToneFinal,
	}
	for days, want := range cases {
		require.Equal(t, want, reminderToneForDays(days), "days=%d", days)
	}
}

func TestReminderMoney(t *testing.T) {
	require.Equal(t, "$37,000", reminderMoney(decimal.NewFromInt(37000), "usd"))
	require.Equal(t, "$1,234.50", reminderMoney(decimal.RequireFromString("1234.5"), "usd"))
	require.Equal(t, "$999", reminderMoney(decimal.NewFromInt(999), "USD"))
}

func overdueGroupFor(now time.Time, numbers map[string]int64, daysAgo map[string]int) overdueGroup {
	g := overdueGroup{customerID: "c1", currency: "usd"}
	for number, amount := range numbers {
		due := now.Add(-time.Duration(daysAgo[number]) * 24 * time.Hour)
		r := openReceivable(number, "c1", "usd", amount, &due)
		r.Number = number
		g.rows = append(g.rows, r)
	}
	g.rows = groupReceivables(g.rows)[0].rows
	return g
}

func TestRenderReminder_Templates(t *testing.T) {
	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	g := overdueGroupFor(now, map[string]int64{"INV-1": 30000, "INV-2": 7000}, map[string]int{"INV-1": 10, "INV-2": 0})

	subject, body, err := renderReminder(types.ReminderToneFriendly, "Acme", "Flex", g, now)
	require.NoError(t, err)
	require.Equal(t, "2 invoices past due: $37,000", subject)
	require.Contains(t, body, "Hi Acme,")
	require.Contains(t, body, "  - INV-1: $30,000 (due Nov 8, 10 days overdue)")
	require.Contains(t, body, "  - INV-2: $7,000 (due Nov 18, overdue today)")
	require.Contains(t, body, "Total outstanding: $37,000")
	require.Contains(t, body, "pay by bank transfer")
	require.True(t, strings.HasSuffix(body, "Thanks,\nFlex Billing\n"))

	firmSubject, firmBody, err := renderReminder(types.ReminderToneFirm, "Acme", "Flex", g, now)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(firmSubject, "Reminder: "))
	require.Contains(t, firmBody, "Our records show")

	finalSubject, finalBody, err := renderReminder(types.ReminderToneFinal, "Acme", "Flex", g, now)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(finalSubject, "Final notice: "))
	require.Contains(t, finalBody, "This is a final notice")

	single := overdueGroupFor(now, map[string]int64{"INV-9": 500}, map[string]int{"INV-9": 1})
	singleSubject, singleBody, err := renderReminder(types.ReminderToneFriendly, "Acme", "Flex", single, now)
	require.NoError(t, err)
	require.Equal(t, "Invoice INV-9 is past due: $500", singleSubject)
	require.Contains(t, singleBody, "the invoice below is past due")
	require.Contains(t, singleBody, "1 day overdue")
}

func TestReminderID_RoundTripAndStrictParse(t *testing.T) {
	id := formatReminderID("usd", "cust_01ABC-x")
	require.Equal(t, "rmd_usd_cust_01ABC-x", id)
	currency, customerID, ok := parseReminderID(id)
	require.True(t, ok)
	require.Equal(t, "usd", currency)
	require.Equal(t, "cust_01ABC-x", customerID)

	for _, bad := range []string{
		"", "rmd_", "rmd_usd", "rmd_usd_", "usd_cust_1", "rmd__cust_1", "rmd_USD_cust_1", "rmd_u_cust_1",
		"rmd_usd_cust 1", "rmd_usd_cust/1", "rmd_usd_cust.1", "rmd_usd_" + strings.Repeat("a", 51), "rmd_abcdefghijk_cust_1",
	} {
		_, _, ok := parseReminderID(bad)
		require.False(t, ok, "%q must not parse", bad)
	}
}

type reminderFixture struct {
	*collectionsFixture
	tenants *testutil.InMemoryTenantStore
	svc     ReminderService
}

func newReminderFixture(t *testing.T, ctx context.Context) *reminderFixture {
	t.Helper()
	base := newCollectionsFixture()
	f := &reminderFixture{collectionsFixture: base, tenants: testutil.NewInMemoryTenantStore()}
	require.NoError(t, f.tenants.Create(ctx, &tenant.Tenant{ID: types.GetTenantID(ctx), Name: "Flex"}))
	f.svc = f.serviceWith(base.reminders)
	return f
}

func (f *reminderFixture) serviceWith(repo collections.ReminderDecisionRepository) ReminderService {
	return NewReminderService(ServiceParams{
		CustomerRepo: f.customers, ReminderDecisionRepo: repo, ReceivableSource: f.source, TenantRepo: f.tenants,
	})
}

func (f *reminderFixture) addCustomerWithEmail(t *testing.T, ctx context.Context, id, name, email string) {
	t.Helper()
	require.NoError(t, f.customers.Create(ctx, &customer.Customer{ID: id, Name: name, Email: email, BaseModel: types.GetDefaultBaseModel(ctx)}))
}

func (f *reminderFixture) overdueInvoice(t *testing.T, ctx context.Context, id, customerID string, amount int64, daysAgo int) {
	t.Helper()
	due := clock.Now().Add(-time.Duration(daysAgo)*24*time.Hour - time.Hour)
	f.addInvoice(t, ctx, id, func(i *invoice.Invoice) {
		i.CustomerID = customerID
		i.DueDate = &due
		i.AmountDue = decimal.NewFromInt(amount)
		i.AmountRemaining = decimal.NewFromInt(amount)
	})
}

func (f *reminderFixture) pay(t *testing.T, ctx context.Context, id string) {
	t.Helper()
	inv, err := f.invoices.Get(ctx, id)
	require.NoError(t, err)
	inv.PaymentStatus, inv.AmountRemaining, inv.PaidAt = types.PaymentStatusSucceeded, decimal.Zero, lo.ToPtr(clock.Now())
	require.NoError(t, f.invoices.Update(ctx, inv))
}

func (f *reminderFixture) list(t *testing.T, ctx context.Context, req dto.ListRemindersRequest) *dto.ListRemindersResponse {
	t.Helper()
	resp, err := f.svc.List(ctx, &req)
	require.NoError(t, err)
	return resp
}

func draftFor(t *testing.T, resp *dto.ListRemindersResponse, customerID string) dto.ReminderDraftResponse {
	t.Helper()
	d, ok := lo.Find(resp.Drafts, func(d dto.ReminderDraftResponse) bool { return d.CustomerID == customerID })
	require.True(t, ok, "no draft for %s", customerID)
	return d
}

func requireKind(t *testing.T, err error, kind error) {
	t.Helper()
	require.Error(t, err)
	require.True(t, errors.Is(err, kind), "got %v", err)
}

func TestReminders_ReadsWriteNothing(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomerWithEmail(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.addCustomerWithEmail(t, ctx, "cust_2", "Beta", "")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	f.overdueInvoice(t, ctx, "a2", "cust_1", 300, 40)
	f.overdueInvoice(t, ctx, "b1", "cust_2", 50, 70)
	f.addInvoice(t, ctx, "b_eur", func(i *invoice.Invoice) {
		i.CustomerID, i.Currency = "cust_2", "EUR"
		i.DueDate = lo.ToPtr(clock.Now().Add(-48 * time.Hour))
	})
	future := clock.Now().Add(48 * time.Hour)
	f.addInvoice(t, ctx, "not_due", func(i *invoice.Invoice) { i.DueDate = &future })
	f.addInvoice(t, ctx, "draft", func(i *invoice.Invoice) {
		i.InvoiceStatus = types.InvoiceStatusDraft
		i.DueDate = lo.ToPtr(clock.Now().Add(-48 * time.Hour))
	})

	resp := f.list(t, ctx, dto.ListRemindersRequest{})
	require.Len(t, resp.Drafts, 3)
	require.Equal(t, 3, resp.Summary.ToReview)
	require.Equal(t, 4, resp.Summary.InvoiceCount)
	require.Equal(t, 2, resp.Summary.CustomerCount)
	require.Equal(t, []dto.CollectionsCurrencyTotal{
		{Currency: "eur", Amount: decimal.NewFromInt(100)},
		{Currency: "usd", Amount: decimal.NewFromInt(450)},
	}, resp.Summary.OverdueTotals)

	acme := draftFor(t, resp, "cust_1")
	require.Equal(t, "rmd_usd_cust_1", acme.ID)
	require.Equal(t, "Acme", acme.CustomerName)
	require.Equal(t, "ap@acme.test", acme.CustomerEmail)
	require.Equal(t, 2, acme.InvoiceCount)
	require.True(t, decimal.NewFromInt(400).Equal(acme.Amount))
	require.Equal(t, 40, acme.OldestDaysOverdue)
	require.Equal(t, types.ReminderToneFirm, acme.Tone)
	require.Equal(t, types.ReminderStatusPending, acme.Status)
	require.Equal(t, "usd", acme.Currency)
	require.Equal(t, "Oldest invoice is 40 days overdue, so a firm tone.", acme.Why[0])
	require.Contains(t, acme.Body, "Flex Billing")
	require.False(t, acme.Stale)
	require.False(t, acme.Edited)
	require.Nil(t, acme.CreatedAt, "no timestamps until someone acts")
	require.True(t, resp.Drafts[0].Amount.GreaterThanOrEqual(resp.Drafts[1].Amount), "sorted by amount desc")

	one, err := f.svc.Get(ctx, acme.ID)
	require.NoError(t, err)
	require.Equal(t, acme.Subject, one.Subject)
	_, err = f.collectionsFixture.svc.GetBoard(ctx, &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Zero(t, f.reminders.Writes(), "list, get and the board never write")
}

func TestReminders_PendingTextFollowsTheInvoices(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	require.Contains(t, draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1").Subject, "Invoice INV-a1 is past due: $100")

	f.overdueInvoice(t, ctx, "a2", "cust_1", 50, 6)
	d := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1")
	require.Contains(t, d.Subject, "2 invoices past due: $150")
	require.False(t, d.Stale, "an undecided, unedited reminder is never stale")
	require.Zero(t, f.reminders.Writes())
}

func TestReminders_GroupingMatchesBoardOverdueCards(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.addCustomer(t, ctx, "cust_2", "Beta")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	f.overdueInvoice(t, ctx, "a2", "cust_1", 300, 40)
	f.overdueInvoice(t, ctx, "b1", "cust_2", 50, 70)

	board, err := f.collectionsFixture.svc.GetBoard(ctx, &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	overdue := columnByKey(t, board, types.CollectionsColumnOverdue)
	resp := f.list(t, ctx, dto.ListRemindersRequest{})

	require.Len(t, resp.Drafts, overdue.Count)
	for _, card := range overdue.Cards {
		d := draftFor(t, resp, card.CustomerID)
		require.Equal(t, d.ID, card.ReminderID)
		require.True(t, card.Amount.Equal(d.Amount), "amount for %s", card.CustomerID)
		require.Equal(t, card.InvoiceCount, d.InvoiceCount)
		require.Equal(t, *card.DaysOverdue, d.OldestDaysOverdue)
		require.Equal(t, card.Currency, d.Currency)
	}
}

func TestReminders_ApproveFreezesTheText(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	before := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1")

	approved, err := f.svc.Approve(ctx, before.ID)
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusApproved, approved.Status)
	require.NotNil(t, approved.DecidedAt)
	require.NotNil(t, approved.CreatedAt)
	require.Equal(t, before.Subject, approved.Subject)

	rows, err := f.reminders.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, before.Subject, rows[0].FrozenSubject)
	require.Equal(t, before.Body, rows[0].FrozenBody)
	require.Equal(t, types.ReminderToneFriendly, rows[0].FrozenTone)

	f.overdueInvoice(t, ctx, "a2", "cust_1", 50, 6)
	after := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1")
	require.Equal(t, before.Subject, after.Subject, "approved text does not change")
	require.Equal(t, before.Body, after.Body)
	require.True(t, after.Stale)
	require.Equal(t, 2, after.InvoiceCount, "invoices show the live state")
	require.Contains(t, after.Why, "The invoices changed after this draft was written.")
}

func TestReminders_EditedDraftKeepsItsTextAndGoesStale(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	id := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1").ID

	edited, err := f.svc.Update(ctx, id, &dto.UpdateReminderRequest{Subject: lo.ToPtr("My subject")})
	require.NoError(t, err)
	require.True(t, edited.Edited)
	require.False(t, edited.Stale)
	require.Contains(t, edited.Body, "Flex Billing", "the body is still the template")

	f.overdueInvoice(t, ctx, "a2", "cust_1", 50, 6)
	d := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1")
	require.Equal(t, "My subject", d.Subject)
	require.True(t, d.Stale)
	require.Contains(t, d.Body, "INV-a2", "unedited parts follow the invoices")
}

func TestReminders_Transitions(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	id := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1").ID

	_, err := f.svc.Reopen(ctx, id)
	requireKind(t, err, ierr.ErrVersionConflict)

	_, err = f.svc.Approve(ctx, id)
	require.NoError(t, err)
	_, err = f.svc.Approve(ctx, id)
	requireKind(t, err, ierr.ErrVersionConflict)
	_, err = f.svc.Hold(ctx, id, &dto.HoldReminderRequest{Reason: "x"})
	requireKind(t, err, ierr.ErrVersionConflict)
	_, err = f.svc.Skip(ctx, id)
	requireKind(t, err, ierr.ErrVersionConflict)
	_, err = f.svc.Update(ctx, id, &dto.UpdateReminderRequest{Body: lo.ToPtr("x")})
	requireKind(t, err, ierr.ErrVersionConflict)

	reopened, err := f.svc.Reopen(ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, reopened.Status)
	require.Nil(t, reopened.DecidedAt)
	rows, err := f.reminders.ListAll(ctx)
	require.NoError(t, err)
	require.Empty(t, rows, "reopening an unedited decision removes its row")
	_, err = f.svc.Reopen(ctx, id)
	requireKind(t, err, ierr.ErrVersionConflict)

	_, err = f.svc.Hold(ctx, id, &dto.HoldReminderRequest{Reason: "  "})
	requireKind(t, err, ierr.ErrValidation)
	held, err := f.svc.Hold(ctx, id, &dto.HoldReminderRequest{Reason: " Promised to pay Friday "})
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusHeld, held.Status)
	require.Equal(t, "Promised to pay Friday", held.HoldReason)
	require.Contains(t, held.Why, "Held: Promised to pay Friday")
	_, err = f.svc.Hold(ctx, id, &dto.HoldReminderRequest{Reason: "again"})
	requireKind(t, err, ierr.ErrVersionConflict)

	edited, err := f.svc.Update(ctx, id, &dto.UpdateReminderRequest{Body: lo.ToPtr("New body")})
	require.NoError(t, err)
	require.Equal(t, "New body", edited.Body)
	require.Equal(t, types.ReminderStatusHeld, edited.Status)

	skipped, err := f.svc.Skip(ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusSkipped, skipped.Status)
	require.Empty(t, skipped.HoldReason)

	back, err := f.svc.Reopen(ctx, id)
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, back.Status)
	require.Equal(t, "New body", back.Body, "an edited decision keeps its text when reopened")
	require.True(t, back.Edited)
	rows, err = f.reminders.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestReminders_ReopenKeepsEditedStaleDraftAndRewritesUneditedOne(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.addCustomer(t, ctx, "cust_2", "Beta")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	f.overdueInvoice(t, ctx, "b1", "cust_2", 100, 5)
	_, err := f.svc.Update(ctx, "rmd_usd_cust_1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("Mine")})
	require.NoError(t, err)
	_, err = f.svc.Approve(ctx, "rmd_usd_cust_1")
	require.NoError(t, err)
	_, err = f.svc.Approve(ctx, "rmd_usd_cust_2")
	require.NoError(t, err)
	f.overdueInvoice(t, ctx, "a2", "cust_1", 50, 6)
	f.overdueInvoice(t, ctx, "b2", "cust_2", 50, 6)

	edited, err := f.svc.Reopen(ctx, "rmd_usd_cust_1")
	require.NoError(t, err)
	require.True(t, edited.Stale, "edited text still refers to the old invoices")
	require.Equal(t, "Mine", edited.Subject)

	plain, err := f.svc.Reopen(ctx, "rmd_usd_cust_2")
	require.NoError(t, err)
	require.False(t, plain.Stale)
	require.Contains(t, plain.Subject, "2 invoices past due: $150", "an unedited reminder reads from the current invoices again")
}

func TestReminders_UpdateRules(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	d := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1")
	require.Equal(t, types.ReminderToneFriendly, d.Tone)

	_, err := f.svc.Update(ctx, d.ID, &dto.UpdateReminderRequest{})
	requireKind(t, err, ierr.ErrValidation)
	_, err = f.svc.Update(ctx, d.ID, &dto.UpdateReminderRequest{Tone: lo.ToPtr("angry")})
	requireKind(t, err, ierr.ErrValidation)
	_, err = f.svc.Update(ctx, d.ID, &dto.UpdateReminderRequest{Subject: lo.ToPtr(" ")})
	requireKind(t, err, ierr.ErrValidation)

	same, err := f.svc.Update(ctx, d.ID, &dto.UpdateReminderRequest{Tone: lo.ToPtr("friendly"), Subject: lo.ToPtr(d.Subject)})
	require.NoError(t, err)
	require.False(t, same.Edited)
	require.Zero(t, f.reminders.Writes(), "a change that changes nothing writes nothing")

	edited, err := f.svc.Update(ctx, d.ID, &dto.UpdateReminderRequest{Subject: lo.ToPtr("Custom"), Body: lo.ToPtr("Custom body")})
	require.NoError(t, err)
	require.True(t, edited.Edited)

	toned, err := f.svc.Update(ctx, d.ID, &dto.UpdateReminderRequest{Tone: lo.ToPtr("final")})
	require.NoError(t, err)
	require.Equal(t, types.ReminderToneFinal, toned.Tone)
	require.True(t, strings.HasPrefix(toned.Subject, "Final notice: "), "a tone change overwrites edits")
	require.False(t, toned.Edited)
	require.Equal(t, "Tone set to final notice by you (oldest invoice is 5 days overdue).", toned.Why[0])
	for _, line := range toned.Why {
		require.NotContains(t, line, "so a friendly tone")
	}

	back, err := f.svc.Update(ctx, d.ID, &dto.UpdateReminderRequest{Tone: lo.ToPtr("friendly")})
	require.NoError(t, err)
	require.Equal(t, types.ReminderToneFriendly, back.Tone)
	require.Equal(t, "Oldest invoice is 5 days overdue, so a friendly tone.", back.Why[0])
	rows, err := f.reminders.ListAll(ctx)
	require.NoError(t, err)
	require.Empty(t, rows, "going back to the automatic tone leaves no row")
}

func TestReminders_OldDecisionDoesNotApplyToNewInvoices(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	_, err := f.svc.Hold(ctx, "rmd_usd_cust_1", &dto.HoldReminderRequest{Reason: "Promised to pay"})
	require.NoError(t, err)
	f.pay(t, ctx, "a1")

	_, err = f.svc.Get(ctx, "rmd_usd_cust_1")
	requireKind(t, err, ierr.ErrNotFound)
	require.Empty(t, f.list(t, ctx, dto.ListRemindersRequest{}).Drafts, "paid invoices hide the reminder")
	board, err := f.collectionsFixture.svc.GetBoard(ctx, &dto.CollectionsBoardRequest{})
	require.NoError(t, err)
	require.Zero(t, columnByKey(t, board, types.CollectionsColumnHeld).Count)

	f.overdueInvoice(t, ctx, "a3", "cust_1", 70, 3)
	d := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1")
	require.Equal(t, types.ReminderStatusPending, d.Status, "the old hold is not resurrected")
	require.Empty(t, d.HoldReason)

	_, err = f.svc.Skip(ctx, d.ID)
	require.NoError(t, err)
	rows, err := f.reminders.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1, "a new decision replaces the leftover row")
}

func TestReminders_ApprovedReminderOfPaidInvoicesIsHidden(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	_, err := f.svc.Approve(ctx, "rmd_usd_cust_1")
	require.NoError(t, err)
	f.pay(t, ctx, "a1")

	require.Empty(t, f.list(t, ctx, dto.ListRemindersRequest{}).Drafts)
	require.Zero(t, f.list(t, ctx, dto.ListRemindersRequest{}).Summary.Approved)
	_, err = f.svc.Approve(ctx, "rmd_usd_cust_1")
	requireKind(t, err, ierr.ErrNotFound)
}

func TestReminders_UnknownOrMalformedIDsAreNotFound(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	for _, id := range []string{"rmd_usd_cust_missing", "not-an-id", "rmd_USD_cust_1", ""} {
		_, err := f.svc.Get(ctx, id)
		requireKind(t, err, ierr.ErrNotFound)
		_, err = f.svc.Approve(ctx, id)
		requireKind(t, err, ierr.ErrNotFound)
		_, err = f.svc.Hold(ctx, id, &dto.HoldReminderRequest{Reason: "x"})
		requireKind(t, err, ierr.ErrNotFound)
		_, err = f.svc.Reopen(ctx, id)
		requireKind(t, err, ierr.ErrNotFound)
	}
}

func TestReminders_BulkApprove(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	for _, c := range []string{"cust_1", "cust_2", "cust_3"} {
		f.addCustomer(t, ctx, c, c)
		f.overdueInvoice(t, ctx, "inv_"+c, c, 100, 5)
	}
	resp := f.list(t, ctx, dto.ListRemindersRequest{})
	ids := lo.Map(resp.Drafts, func(d dto.ReminderDraftResponse, _ int) string { return d.ID })
	_, err := f.svc.Skip(ctx, draftFor(t, resp, "cust_3").ID)
	require.NoError(t, err)
	writes := f.reminders.Writes()

	_, err = f.svc.BulkApprove(ctx, &dto.BulkApproveRemindersRequest{})
	requireKind(t, err, ierr.ErrValidation)
	_, err = f.svc.BulkApprove(ctx, &dto.BulkApproveRemindersRequest{IDs: []string{ids[0], "rmd_usd_cust_missing"}})
	requireKind(t, err, ierr.ErrNotFound)
	_, err = f.svc.BulkApprove(ctx, &dto.BulkApproveRemindersRequest{IDs: []string{ids[0], "garbage"}})
	requireKind(t, err, ierr.ErrNotFound)
	require.Equal(t, writes, f.reminders.Writes(), "a failed bulk request approves nothing")

	out, err := f.svc.BulkApprove(ctx, &dto.BulkApproveRemindersRequest{IDs: append(ids, ids[0])})
	require.NoError(t, err)
	require.Len(t, out.Drafts, 2, "the skipped reminder is left out and duplicates count once")
	for _, d := range out.Drafts {
		require.Equal(t, types.ReminderStatusApproved, d.Status)
	}
	summary := f.list(t, ctx, dto.ListRemindersRequest{}).Summary
	require.Equal(t, 2, summary.Approved)
	require.Equal(t, 1, summary.Skipped)
}

// racingRepo runs a competing change once, right before the first write it receives.
type racingRepo struct {
	collections.ReminderDecisionRepository
	race func()
}

func (r *racingRepo) before() {
	if race := r.race; race != nil {
		r.race = nil
		race()
	}
}

func (r *racingRepo) Create(ctx context.Context, d *collections.ReminderDecision) error {
	r.before()
	return r.ReminderDecisionRepository.Create(ctx, d)
}

func (r *racingRepo) Update(ctx context.Context, d *collections.ReminderDecision) error {
	r.before()
	return r.ReminderDecisionRepository.Update(ctx, d)
}

func TestReminders_ConcurrentApproveGivesOneSuccessAndOneConflict(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)

	slow := &racingRepo{ReminderDecisionRepository: f.reminders}
	slow.race = func() {
		_, err := f.svc.Approve(ctx, "rmd_usd_cust_1")
		require.NoError(t, err)
	}
	_, err := f.serviceWith(slow).Approve(ctx, "rmd_usd_cust_1")
	requireKind(t, err, ierr.ErrVersionConflict)
	rows, err := f.reminders.ListAll(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, types.ReminderStatusApproved, rows[0].Status)
}

func TestReminders_ConcurrentEditsRetryOnFreshData(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	_, err := f.svc.Update(ctx, "rmd_usd_cust_1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("First")})
	require.NoError(t, err)

	slow := &racingRepo{ReminderDecisionRepository: f.reminders}
	slow.race = func() {
		_, err := f.svc.Update(ctx, "rmd_usd_cust_1", &dto.UpdateReminderRequest{Body: lo.ToPtr("Their body")})
		require.NoError(t, err)
	}
	merged, err := f.serviceWith(slow).Update(ctx, "rmd_usd_cust_1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("Second")})
	require.NoError(t, err)
	require.Equal(t, "Second", merged.Subject)
	require.Equal(t, "Their body", merged.Body, "the retry started from the other person's change")
}

func TestReminders_BulkApproveSkipsAReminderChangedMeanwhile(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)

	slow := &racingRepo{ReminderDecisionRepository: f.reminders}
	slow.race = func() {
		_, err := f.svc.Skip(ctx, "rmd_usd_cust_1")
		require.NoError(t, err)
	}
	out, err := f.serviceWith(slow).BulkApprove(ctx, &dto.BulkApproveRemindersRequest{IDs: []string{"rmd_usd_cust_1"}})
	require.NoError(t, err)
	require.Empty(t, out.Drafts)
	require.Equal(t, 1, f.list(t, ctx, dto.ListRemindersRequest{}).Summary.Skipped)
}

func TestReminders_ListFilters(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomerWithEmail(t, ctx, "cust_1", "Acme Robotics", "ap@acme.test")
	f.addCustomerWithEmail(t, ctx, "cust_2", "Beta Labs", "")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	f.overdueInvoice(t, ctx, "b1", "cust_2", 200, 70)
	_, err := f.svc.Approve(ctx, "rmd_usd_cust_1")
	require.NoError(t, err)

	require.Len(t, f.list(t, ctx, dto.ListRemindersRequest{Status: "approved"}).Drafts, 1)
	require.Len(t, f.list(t, ctx, dto.ListRemindersRequest{Status: "pending"}).Drafts, 1)
	require.Len(t, f.list(t, ctx, dto.ListRemindersRequest{Tone: "final"}).Drafts, 1)
	require.Len(t, f.list(t, ctx, dto.ListRemindersRequest{Q: "ROBOT"}).Drafts, 1)
	require.Len(t, f.list(t, ctx, dto.ListRemindersRequest{Q: "inv-b1"}).Drafts, 1)
	require.Len(t, f.list(t, ctx, dto.ListRemindersRequest{CustomerID: "cust_2"}).Drafts, 1)
	require.Len(t, f.list(t, ctx, dto.ListRemindersRequest{Currency: "USD"}).Drafts, 2)
	require.Empty(t, f.list(t, ctx, dto.ListRemindersRequest{Currency: "eur"}).Drafts)
	require.Equal(t, 2, f.list(t, ctx, dto.ListRemindersRequest{Status: "held"}).Summary.CustomerCount, "summary ignores filters")

	_, err = f.svc.List(ctx, &dto.ListRemindersRequest{Status: "nope"})
	requireKind(t, err, ierr.ErrValidation)
	_, err = f.svc.List(ctx, &dto.ListRemindersRequest{Tone: "nope"})
	requireKind(t, err, ierr.ErrValidation)
}

func TestReminders_TenantAndEnvironmentIsolation(t *testing.T) {
	ctxA := collectionsCtx("tenant_a", "env_a")
	ctxOtherTenant := collectionsCtx("tenant_b", "env_a")
	ctxOtherEnv := collectionsCtx("tenant_a", "env_b")
	f := newReminderFixture(t, ctxA)
	f.addCustomer(t, ctxA, "cust_1", "Acme")
	f.overdueInvoice(t, ctxA, "a1", "cust_1", 100, 5)
	f.overdueInvoice(t, ctxOtherTenant, "t1", "cust_1", 100, 5)
	f.overdueInvoice(t, ctxOtherEnv, "e1", "cust_1", 100, 5)
	_, err := f.svc.Hold(ctxA, "rmd_usd_cust_1", &dto.HoldReminderRequest{Reason: "mine"})
	require.NoError(t, err)

	mine := draftFor(t, f.list(t, ctxA, dto.ListRemindersRequest{}), "cust_1")
	require.Equal(t, types.ReminderStatusHeld, mine.Status)
	require.Equal(t, []string{"a1"}, lo.Map(mine.Invoices, func(i dto.ReminderInvoice, _ int) string { return i.ID }))

	for _, ctx := range []context.Context{ctxOtherTenant, ctxOtherEnv} {
		other := f.list(t, ctx, dto.ListRemindersRequest{})
		require.Len(t, other.Drafts, 1)
		require.Equal(t, types.ReminderStatusPending, other.Drafts[0].Status, "another tenant or environment never sees the decision")
		require.Equal(t, 1, other.Summary.ToReview)
	}
	require.Equal(t, 1, f.list(t, ctxA, dto.ListRemindersRequest{}).Summary.Held)
}

func TestReminders_ListIsPagedWithTheSameSummaryOnEveryPage(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	for i := 1; i <= 7; i++ {
		id := fmt.Sprintf("cust_%d", i)
		f.addCustomer(t, ctx, id, id)
		f.overdueInvoice(t, ctx, "inv_"+id, id, int64(100*i), 5)
	}
	f.addCustomer(t, ctx, "cust_tie_a", "a")
	f.addCustomer(t, ctx, "cust_tie_b", "b")
	f.overdueInvoice(t, ctx, "inv_tie_a", "cust_tie_a", 700, 5)
	f.overdueInvoice(t, ctx, "inv_tie_b", "cust_tie_b", 700, 5)
	_, err := f.svc.Approve(ctx, "rmd_usd_cust_5")
	require.NoError(t, err)

	asOf := f.list(t, ctx, dto.ListRemindersRequest{}).AsOf.Format(time.RFC3339Nano)
	everything := f.list(t, ctx, dto.ListRemindersRequest{Limit: dto.RemindersMaxLimit, AsOf: asOf})
	ids := lo.Map(everything.Drafts, func(d dto.ReminderDraftResponse, _ int) string { return d.ID })
	require.Equal(t, []string{"rmd_usd_cust_7", "rmd_usd_cust_tie_a", "rmd_usd_cust_tie_b", "rmd_usd_cust_6", "rmd_usd_cust_5", "rmd_usd_cust_4", "rmd_usd_cust_3", "rmd_usd_cust_2", "rmd_usd_cust_1"}, ids,
		"amount desc; equal amounts by customer id")

	for _, size := range []int{1, 2, 3, 4, 7, 15} {
		var seen []string
		for page := 1; page <= 20; page++ {
			resp, err := f.svc.List(ctx, &dto.ListRemindersRequest{Limit: size, Page: page, AsOf: asOf})
			require.NoError(t, err)
			require.Equal(t, page, resp.Page)
			require.Equal(t, everything.Summary, resp.Summary, "the summary does not depend on the page")
			seen = append(seen, lo.Map(resp.Drafts, func(d dto.ReminderDraftResponse, _ int) string { return d.ID })...)
			if !resp.HasMore {
				break
			}
			require.Len(t, resp.Drafts, size)
		}
		require.Equal(t, ids, seen, "page size %d", size)
	}

	exact, err := f.svc.List(ctx, &dto.ListRemindersRequest{Limit: 3, Page: 3})
	require.NoError(t, err)
	require.Len(t, exact.Drafts, 3)
	require.False(t, exact.HasMore, "the last page fits exactly")
	past, err := f.svc.List(ctx, &dto.ListRemindersRequest{Limit: 3, Page: 4})
	require.NoError(t, err)
	require.Empty(t, past.Drafts)
	require.False(t, past.HasMore)

	for name, req := range map[string]dto.ListRemindersRequest{
		"limit above the maximum": {Limit: 101},
		"negative page":           {Page: -1},
		"too deep":                {Page: 51, Limit: 100},
		"too deep by default":     {Page: 334},
		"malformed as_of":         {AsOf: "nope"},
		"as_of too old":           {AsOf: time.Now().Add(-25 * time.Hour).Format(time.RFC3339Nano)},
		"as_of in the future":     {AsOf: time.Now().Add(time.Hour).Format(time.RFC3339Nano)},
	} {
		_, err := f.svc.List(ctx, &req)
		requireKind(t, err, ierr.ErrValidation)
		_ = name
	}
	_, err = f.svc.List(ctx, &dto.ListRemindersRequest{Page: 50, Limit: 100})
	require.NoError(t, err, "the deepest allowed page")

	pending, err := f.svc.List(ctx, &dto.ListRemindersRequest{Limit: 2, Status: "pending", Page: 2})
	require.NoError(t, err)
	require.Equal(t, []string{"cust_tie_b", "cust_6"}, lo.Map(pending.Drafts, func(d dto.ReminderDraftResponse, _ int) string { return d.CustomerID }))
	require.True(t, pending.HasMore)
	require.Equal(t, 9, pending.Summary.CustomerCount, "summary ignores filters")
}

func TestReminders_ListCapsInvoicesAndDetailReturnsAll(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	for i := 0; i < 30; i++ {
		f.overdueInvoice(t, ctx, fmt.Sprintf("inv_%02d", i), "cust_1", 10, 5+i)
	}
	listed := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{}), "cust_1")
	require.Equal(t, 30, listed.InvoiceCount)
	require.Len(t, listed.Invoices, dto.ReminderListInvoiceCap)
	require.True(t, decimal.NewFromInt(300).Equal(listed.Amount), "totals cover every invoice")

	detail, err := f.svc.Get(ctx, listed.ID)
	require.NoError(t, err)
	require.Len(t, detail.Invoices, 30)
}

func TestReminders_SearchPicksWholeGroups(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.addCustomer(t, ctx, "cust_1", "Acme")
	f.overdueInvoice(t, ctx, "a1", "cust_1", 100, 5)
	f.overdueInvoice(t, ctx, "a2", "cust_1", 300, 6)

	d := draftFor(t, f.list(t, ctx, dto.ListRemindersRequest{Q: "inv-a2"}), "cust_1")
	require.Equal(t, 2, d.InvoiceCount, "the group shows every invoice, not only the match")
	require.True(t, decimal.NewFromInt(400).Equal(d.Amount))
}

func TestReminders_BulkApproveRejectsTooManyIDs(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	ids := make([]string, dto.RemindersMaxBulk+1)
	for i := range ids {
		ids[i] = fmt.Sprintf("rmd_usd_cust_%d", i)
	}
	_, err := f.svc.BulkApprove(ctx, &dto.BulkApproveRemindersRequest{IDs: ids})
	requireKind(t, err, ierr.ErrValidation)
	require.Zero(t, f.reminders.Writes())
}

func TestReminders_ToneFilterMatchesTheBands(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	for id, days := range map[string]int{"cust_0": 0, "cust_30": 30, "cust_31": 31, "cust_60": 60, "cust_61": 61} {
		f.addCustomer(t, ctx, id, id)
		f.overdueInvoice(t, ctx, "inv_"+id, id, 10, days)
	}
	names := func(tone string) []string {
		resp := f.list(t, ctx, dto.ListRemindersRequest{Tone: tone})
		out := lo.Map(resp.Drafts, func(d dto.ReminderDraftResponse, _ int) string { return d.CustomerID })
		sort.Strings(out)
		return out
	}
	require.Equal(t, []string{"cust_0", "cust_30"}, names("friendly"))
	require.Equal(t, []string{"cust_31", "cust_60"}, names("firm"))
	require.Equal(t, []string{"cust_61"}, names("final"))
	for _, d := range f.list(t, ctx, dto.ListRemindersRequest{}).Drafts {
		require.Equal(t, reminderToneForDays(d.OldestDaysOverdue), d.Tone, "the filter and the shown tone agree for %s", d.CustomerID)
	}
}

func TestReminders_ListDefaultsToFifteen(t *testing.T) {
	ctx := collectionsCtx("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("cust_%02d", i)
		f.addCustomer(t, ctx, id, id)
		f.overdueInvoice(t, ctx, "inv_"+id, id, int64(100+i), 5)
	}
	first := f.list(t, ctx, dto.ListRemindersRequest{})
	require.Len(t, first.Drafts, 15)
	require.True(t, first.HasMore)
	require.Equal(t, 1, first.Page)
	require.Equal(t, 20, first.Summary.ToReview)

	rest, err := f.svc.List(ctx, &dto.ListRemindersRequest{Page: 2})
	require.NoError(t, err)
	require.Len(t, rest.Drafts, 5)
	require.False(t, rest.HasMore)

	big := f.list(t, ctx, dto.ListRemindersRequest{Limit: dto.RemindersMaxLimit})
	require.Len(t, big.Drafts, 20, "a caller that needs more passes limit")
}
