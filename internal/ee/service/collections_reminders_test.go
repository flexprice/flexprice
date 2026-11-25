package service

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cockroachdb/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/customer"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/email/emailtypes"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/storage"
	"github.com/flexprice/flexprice/internal/storage/storagetypes"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
)

const day = 24 * time.Hour

func setStatus(status types.ReminderStatus, reason string) *dto.UpdateReminderRequest {
	return &dto.UpdateReminderRequest{Status: status, StatusReason: reason}
}

func bulkStatus(status types.ReminderStatus, ids []string) *dto.BulkUpdateRemindersRequest {
	return &dto.BulkUpdateRemindersRequest{InvoiceIDs: ids, UpdateReminderRequest: *setStatus(status, "")}
}

func reminderCtxFor(tenantID, env string) context.Context {
	ctx := context.WithValue(context.Background(), types.CtxTenantID, tenantID)
	return context.WithValue(ctx, types.CtxEnvironmentID, env)
}

func requireKind(t *testing.T, err error, kind error) {
	t.Helper()
	require.Error(t, err)
	require.True(t, errors.Is(err, kind), "got %v", err)
}

// countingEmailLogs counts List calls, to show a page of reminders reads its emails in one query.
type countingEmailLogs struct {
	emaillog.Repository
	calls *int
}

func (c *countingEmailLogs) List(ctx context.Context, filter *types.EmailLogFilter) ([]*emaillog.EmailLog, error) {
	*c.calls++
	return c.Repository.List(ctx, filter)
}

// noStorage is an object storage resolver with nothing behind it, as a local run has: invoice PDFs cannot be produced.
type noStorage struct{ storage.Resolver }

func (noStorage) ForPlatform(context.Context, storage.Purpose) (storagetypes.Storage, error) {
	return nil, ierr.NewError("no object storage").Mark(ierr.ErrServiceUnavailable)
}

// sentEmails is an email provider that keeps what it was asked to send.
type sentEmails struct {
	mu   sync.Mutex
	list []emailtypes.OutboundEmail
	// down makes every send fail as a provider outage would, so emails stay queued.
	down bool
}

func (p *sentEmails) Name() types.EmailProvider { return types.EmailProviderLocal }

func (p *sentEmails) Send(_ context.Context, msg emailtypes.OutboundEmail) (emailtypes.SendResult, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.down {
		return emailtypes.SendResult{}, ierr.NewError("provider unavailable").Mark(ierr.ErrServiceUnavailable)
	}
	p.list = append(p.list, msg)
	return emailtypes.SendResult{ProviderMessageID: "msg_" + msg.IdempotencyKey}, nil
}

func (p *sentEmails) ParseEvents(context.Context, []byte, http.Header) ([]emailtypes.DeliveryEvent, error) {
	return nil, nil
}

func (p *sentEmails) all() []emailtypes.OutboundEmail {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]emailtypes.OutboundEmail(nil), p.list...)
}

type reminderFixture struct {
	invoices  *testutil.InMemoryInvoiceStore
	customers *testutil.InMemoryCustomerStore
	tenants   *testutil.InMemoryTenantStore
	reminders *testutil.InMemoryInvoiceReminderStore
	board     *fakeBoard
	svc       ReminderService
	logs      *testutil.InMemoryEmailLogStore
	settings  *testutil.InMemorySettingsStore
	emailOn   bool
	sent      *sentEmails
	lists     int
}

func newReminderFixture(t *testing.T, ctx context.Context) *reminderFixture {
	t.Helper()
	invoices := testutil.NewInMemoryInvoiceStore()
	f := &reminderFixture{
		invoices: invoices, customers: testutil.NewInMemoryCustomerStore(), tenants: testutil.NewInMemoryTenantStore(),
		reminders: testutil.NewInMemoryInvoiceReminderStore(invoices), board: &fakeBoard{},
		logs: testutil.NewInMemoryEmailLogStore(), settings: testutil.NewInMemorySettingsStore(), sent: &sentEmails{},
	}
	require.NoError(t, f.tenants.Create(ctx, &tenant.Tenant{ID: types.GetTenantID(ctx), Name: "Flex"}))
	f.setCollections(t, ctx, true)
	f.withEmail(t, ctx, emailsOn)
	return f
}

// setCollections turns Collections on or off in the fixture's environment.
func (f *reminderFixture) setCollections(t *testing.T, ctx context.Context, enabled bool) {
	t.Helper()
	_ = f.settings.DeleteByKey(ctx, types.SettingKeyCollectionsConfig)
	require.NoError(t, f.settings.Create(ctx, &settings.Setting{
		ID: types.GenerateUUIDWithPrefix("setting"), Key: types.SettingKeyCollectionsConfig, Value: map[string]interface{}{"enabled": enabled},
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}))
}

func (f *reminderFixture) serviceWith(repo collections.InvoiceReminderRepository) ReminderService {
	return NewReminderService(f.params(repo))
}

func (f *reminderFixture) params(repo collections.InvoiceReminderRepository) ServiceParams {
	log := logger.NewNoopLogger()
	return ServiceParams{
		Logger: log, DB: testutil.NewMockPostgresClient(log), CustomerRepo: f.customers, TenantRepo: f.tenants, InvoiceRepo: f.invoices,
		InvoiceReminderRepo: repo, CollectionsRepo: f.board, SettingsRepo: f.settings, EmailLogRepo: &countingEmailLogs{Repository: f.logs, calls: &f.lists}, EmailProvider: f.sent,
		InvoiceLineItemRepo: testutil.NewInMemoryInvoiceLineItemStore(), PaymentRepo: testutil.NewInMemoryPaymentStore(), WalletRepo: testutil.NewInMemoryWalletStore(),
		TaxAppliedRepo: testutil.NewInMemoryTaxAppliedStore(), TaxRateRepo: testutil.NewInMemoryTaxRateStore(),
		ConnectionRepo: testutil.NewInMemoryConnectionStore(), StorageResolver: noStorage{},
		Config: &config.Configuration{Email: config.EmailConfig{Enabled: f.emailOn, Provider: string(types.EmailProviderLocal), SenderLocalPart: "billing", SenderDomain: "mail.test"}},
	}
}

func (f *reminderFixture) customer(t *testing.T, ctx context.Context, id, name, email string) {
	t.Helper()
	require.NoError(t, f.customers.Create(ctx, &customer.Customer{ID: id, Name: name, Email: email, BaseModel: types.GetDefaultBaseModel(ctx)}))
}

// overdue adds an open finalized invoice due at the start of the UTC day daysAgo days back, so it is daysAgo calendar days overdue at any time of day.
func (f *reminderFixture) overdue(t *testing.T, ctx context.Context, id, customerID string, amount int64, daysAgo int) {
	t.Helper()
	due := types.UTCDay(clock.Now()).Add(-time.Duration(daysAgo) * day)
	f.invoice(t, ctx, id, func(i *invoice.Invoice) {
		i.CustomerID, i.DueDate = customerID, &due
		i.AmountDue, i.AmountRemaining = decimal.NewFromInt(amount), decimal.NewFromInt(amount)
	})
}

func (f *reminderFixture) invoice(t *testing.T, ctx context.Context, id string, mutate func(*invoice.Invoice)) {
	t.Helper()
	inv := &invoice.Invoice{
		ID: id, CustomerID: "cust_1", InvoiceStatus: types.InvoiceStatusFinalized, PaymentStatus: types.PaymentStatusPending,
		Currency: "usd", AmountDue: decimal.NewFromInt(100), AmountRemaining: decimal.NewFromInt(100),
		InvoiceNumber: lo.ToPtr("INV-" + id), BaseModel: types.GetDefaultBaseModel(ctx),
	}
	mutate(inv)
	require.NoError(t, f.invoices.Create(ctx, inv))
}

func (f *reminderFixture) pay(t *testing.T, ctx context.Context, id string) {
	t.Helper()
	inv, err := f.invoices.Get(ctx, id)
	require.NoError(t, err)
	inv.PaymentStatus, inv.AmountRemaining, inv.PaidAt = types.PaymentStatusSucceeded, decimal.Zero, lo.ToPtr(clock.Now())
	require.NoError(t, f.invoices.Update(ctx, inv))
}

func TestReminder_ToneBands(t *testing.T) {
	for days, want := range map[int]types.ReminderTone{
		0: types.ReminderToneFriendly, 1: types.ReminderToneFriendly, 30: types.ReminderToneFriendly,
		31: types.ReminderToneFirm, 60: types.ReminderToneFirm, 61: types.ReminderToneFinal, 200: types.ReminderToneFinal,
	} {
		require.Equal(t, want, reminderToneForDays(days), "days=%d", days)
	}
}

func TestReminder_MoneyAndText(t *testing.T) {
	require.Equal(t, "$37,000", reminderMoney(decimal.NewFromInt(37000), "usd"))
	require.Equal(t, "$1,234.50", reminderMoney(decimal.RequireFromString("1234.5"), "usd"))

	now := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	inv := reminderInvoice{number: "INV-9", currency: "usd", remaining: decimal.NewFromInt(4200), due: now.Add(-10 * day)}
	subject, body, err := renderReminder(types.ReminderToneFriendly, "Acme", "Flex", inv, now)
	require.NoError(t, err)
	require.Equal(t, "Invoice INV-9 is past due: $4,200", subject)
	require.Contains(t, body, "Hi Acme,")
	require.Contains(t, body, "  - INV-9: $4,200 (due Nov 8, 10 days overdue)")
	require.True(t, strings.HasSuffix(body, "Thanks,\nFlex Billing\n"))

	firmSubject, firmBody, err := renderReminder(types.ReminderToneFirm, "Acme", "Flex", inv, now)
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(firmSubject, "Reminder: "))
	require.Contains(t, firmBody, "Our records show invoice INV-9")
	finalSubject, finalBody, err := renderReminder(types.ReminderToneFinal, "Acme", "Flex", inv, now.Add(80*day))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(finalSubject, "Final notice: "))
	require.Contains(t, finalBody, "This is a final notice")

	today := reminderInvoice{number: "INV-1", currency: "usd", remaining: decimal.NewFromInt(5), due: now}
	_, todayBody, err := renderReminder(types.ReminderToneFriendly, "A", "T", today, now)
	require.NoError(t, err)
	require.Contains(t, todayBody, "overdue today")
}

func TestReminder_GetRendersPendingWithoutWriting(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme Robotics", "ap@acme.example")
	f.overdue(t, ctx, "a1", "cust_1", 4200, 40)

	v, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, "a1", v.ID)
	require.Equal(t, "a1", v.InvoiceID)
	require.Equal(t, "INV-a1", v.InvoiceNumber)
	require.Equal(t, "Acme Robotics", v.CustomerName)
	require.Equal(t, "ap@acme.example", v.CustomerEmail)
	require.Equal(t, 40, v.DaysOverdue)
	require.Equal(t, types.ReminderStatusPending, v.Status)
	require.Equal(t, types.ReminderToneFirm, v.Tone)
	require.Equal(t, "Reminder: Invoice INV-a1 is past due: $4,200", v.Subject)
	require.Contains(t, v.Body, "Hi Acme Robotics,")
	require.Contains(t, v.Body, "Flex Billing")
	require.False(t, v.Edited)
	require.Empty(t, v.OriginalSubject)
	require.Nil(t, v.CreatedAt, "no timestamps until someone acts")
	require.Equal(t, "Invoice is 40 days overdue, so a firm tone.", v.ToneReason)
	require.Zero(t, f.reminders.Writes(), "reads store nothing")
}

func TestReminder_NotFoundWhenNotOpenAndOverdue(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "paid", "cust_1", 100, 5)
	f.pay(t, ctx, "paid")
	soon := clock.Now().Add(day)
	f.invoice(t, ctx, "not_due", func(i *invoice.Invoice) { i.DueDate = &soon })
	f.invoice(t, ctx, "draft", func(i *invoice.Invoice) {
		i.InvoiceStatus, i.DueDate = types.InvoiceStatusDraft, lo.ToPtr(clock.Now().Add(-day))
	})
	f.invoice(t, ctx, "no_due", func(i *invoice.Invoice) { i.DueDate = nil })

	for _, id := range []string{"paid", "not_due", "draft", "no_due", "missing", ""} {
		_, err := f.svc.Get(ctx, id)
		requireKind(t, err, ierr.ErrNotFound)
		_, err = f.svc.UpdateReminder(ctx, id, setStatus(types.ReminderStatusApproved, ""))
		requireKind(t, err, ierr.ErrNotFound)
		_, err = f.svc.UpdateReminder(ctx, id, setStatus(types.ReminderStatusHeld, "x"))
		requireKind(t, err, ierr.ErrNotFound)
		_, err = f.svc.UpdateReminder(ctx, id, setStatus(types.ReminderStatusSkipped, ""))
		requireKind(t, err, ierr.ErrNotFound)
		_, err = f.svc.UpdateReminder(ctx, id, setStatus(types.ReminderStatusPending, ""))
		requireKind(t, err, ierr.ErrNotFound)
	}
	require.Zero(t, f.reminders.Writes())

	_, err := f.svc.Get(ctx, "missing")
	require.Contains(t, errors.FlattenHints(err), "missing", "the not-found message names the invoice")
}

func TestReminder_EditStoresOriginalAndMail(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	before, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)

	same, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Subject: lo.ToPtr(before.Subject), Tone: lo.ToPtr("friendly")})
	require.NoError(t, err)
	require.False(t, same.Edited)
	require.Zero(t, f.reminders.Writes(), "a change that changes nothing writes nothing")

	edited, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("  My subject  "), Body: lo.ToPtr("My body")})
	require.NoError(t, err)
	require.True(t, edited.Edited)
	require.Equal(t, "My subject", edited.Subject)
	require.Equal(t, "My body", edited.Body)
	require.Equal(t, before.Subject, edited.OriginalSubject)
	require.Equal(t, before.Body, edited.OriginalBody)
	require.Equal(t, before.Tone, edited.OriginalTone)
	require.NotNil(t, edited.CreatedAt)
	require.Equal(t, types.ReminderStatusPending, edited.Status)

	stored := f.reminders.All(ctx)
	require.Len(t, stored, 1)
	require.Equal(t, "a1", stored[0].InvoiceID)
	require.True(t, strings.HasPrefix(stored[0].ID, "irem"))
	require.Equal(t, before.Subject, stored[0].Original().OriginalSubject)

	again, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, "My subject", again.Subject, "an edited reminder is read back as edited")

	back, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Subject: lo.ToPtr(before.Subject), Body: lo.ToPtr(before.Body)})
	require.NoError(t, err)
	require.False(t, back.Edited, "editing back to the original is no longer an edit")

	for name, req := range map[string]dto.UpdateReminderRequest{
		"nothing": {}, "bad tone": {Tone: lo.ToPtr("angry")}, "blank subject": {Subject: lo.ToPtr(" ")}, "blank body": {Body: lo.ToPtr("")},
	} {
		_, err := f.svc.UpdateReminder(ctx, "a1", &req)
		requireKind(t, err, ierr.ErrValidation)
		_ = name
	}
}

func TestReminder_ToneChangeOverwritesHandEditsThenAppliesTheRequestOnTop(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	original, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	_, err = f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("Hand written"), Body: lo.ToPtr("Hand body")})
	require.NoError(t, err)

	toned, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Tone: lo.ToPtr("final")})
	require.NoError(t, err)
	require.Equal(t, types.ReminderToneFinal, toned.Tone)
	require.True(t, strings.HasPrefix(toned.Subject, "Final notice: "), "the tone's template replaces the hand edit")
	require.Contains(t, toned.Body, "This is a final notice")
	require.True(t, toned.Edited)
	require.Equal(t, original.Subject, toned.OriginalSubject, "the system's version is kept")
	require.Equal(t, "Tone set to final notice by you (invoice is 5 days overdue).", toned.ToneReason)

	both, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Tone: lo.ToPtr("firm"), Subject: lo.ToPtr("Mine again")})
	require.NoError(t, err)
	require.Equal(t, types.ReminderToneFirm, both.Tone)
	require.Equal(t, "Mine again", both.Subject, "a subject in the same request is applied on top of the new tone")
	require.Contains(t, both.Body, "Our records show")
}

func TestReminder_Transitions(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)

	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
	requireKind(t, err, ierr.ErrVersionConflict)
	approved, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusApproved, approved.Status)
	require.NotNil(t, approved.ApprovedAt)
	require.False(t, approved.Edited)
	stored := f.reminders.All(ctx)[0]
	require.Equal(t, stored.Original().OriginalBody, stored.Body, "approving an untouched reminder stores the approved text as both")

	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	requireKind(t, err, ierr.ErrVersionConflict)
	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusHeld, "x"))
	requireKind(t, err, ierr.ErrVersionConflict)
	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusSkipped, ""))
	requireKind(t, err, ierr.ErrVersionConflict)
	_, err = f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Body: lo.ToPtr("x")})
	requireKind(t, err, ierr.ErrVersionConflict)

	reopened, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, reopened.Status)
	require.Nil(t, reopened.ApprovedAt)
	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
	requireKind(t, err, ierr.ErrVersionConflict)

	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusHeld, "  "))
	requireKind(t, err, ierr.ErrValidation)
	held, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusHeld, " Promised Friday "))
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusHeld, held.Status)
	require.Equal(t, "Promised Friday", held.StatusReason)
	require.NotContains(t, held.ToneReason, "Promised Friday", "tone_reason explains the tone only; the reason is status_reason")
	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusHeld, "again"))
	requireKind(t, err, ierr.ErrVersionConflict)

	edited, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Body: lo.ToPtr("New body")})
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusHeld, edited.Status, "a held reminder can be edited and stays held")

	skipped, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusSkipped, " not worth it "))
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusSkipped, skipped.Status)
	require.Equal(t, "not worth it", skipped.StatusReason)

	back, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
	require.NoError(t, err)
	require.Empty(t, back.StatusReason, "reopening clears the reason")
	require.Equal(t, "New body", back.Body, "and keeps the edits")
	require.True(t, back.Edited)
	require.Len(t, f.reminders.All(ctx), 1, "one row per invoice throughout")
}

func TestReminder_ApproveKeepsEditsAndRewritesAnUneditedHeldReminder(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "edited", "cust_1", 100, 5)
	f.overdue(t, ctx, "plain", "cust_1", 100, 5)

	_, err := f.svc.UpdateReminder(ctx, "edited", &dto.UpdateReminderRequest{Body: lo.ToPtr("Custom body")})
	require.NoError(t, err)
	approved, err := f.svc.UpdateReminder(ctx, "edited", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Equal(t, "Custom body", approved.Body, "the approved text is the edited one")
	require.True(t, approved.Edited)
	require.NotEqual(t, "Custom body", approved.OriginalBody)

	_, err = f.svc.UpdateReminder(ctx, "plain", setStatus(types.ReminderStatusHeld, "wait"))
	require.NoError(t, err)
	var stored *collections.InvoiceReminder
	for _, r := range f.reminders.All(ctx) {
		if r.InvoiceID == "plain" {
			stored = r
		}
	}
	require.NotNil(t, stored)
	stale := *stored
	stale.Metadata = collections.NewInvoiceReminderBuilder(&stale).WithOriginal(stale.Tone, stale.Subject, "snapshot taken long ago").Build().Metadata
	stale.Body = "snapshot taken long ago"
	require.NoError(t, f.reminders.Update(ctx, &stale, stored))

	rewritten, err := f.svc.UpdateReminder(ctx, "plain", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Contains(t, rewritten.Body, "Hi Acme", "an unedited held reminder is written fresh when approved")
	require.False(t, rewritten.Edited)
}

func TestReminder_ApproveRefreshesAnUneditedHeldReminderEvenWhenTheRequestRepeatsItsTone(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "plain", "cust_1", 100, 5)

	_, err := f.svc.UpdateReminder(ctx, "plain", setStatus(types.ReminderStatusHeld, "wait"))
	require.NoError(t, err)
	stored := f.reminders.All(ctx)[0]
	stale := *stored
	stale.Body = "snapshot taken long ago"
	stale.Metadata = collections.NewInvoiceReminderBuilder(&stale).WithOriginal(stale.Tone, stale.Subject, "snapshot taken long ago").Build().Metadata
	require.NoError(t, f.reminders.Update(ctx, &stale, stored))

	approve := setStatus(types.ReminderStatusApproved, "")
	approve.Tone = lo.ToPtr(string(types.ReminderToneFriendly))
	got, err := f.svc.UpdateReminder(ctx, "plain", approve)
	require.NoError(t, err)
	require.Contains(t, got.Body, "Hi Acme", "the tone the reviewer saw is not a reason to approve an old snapshot")
	require.NotContains(t, got.Body, "snapshot taken long ago")
	require.False(t, got.Edited)
	require.NotEqual(t, "snapshot taken long ago", f.reminders.All(ctx)[0].Original().OriginalBody, "the baseline is refreshed too")
}

func TestReminder_FirstActionConflictAndStaleUpdateAre409(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)

	racing := &racingReminderRepo{InvoiceReminderRepository: f.reminders}
	racing.race = func() {
		_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
		require.NoError(t, err)
	}
	_, err := f.serviceWith(racing).UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusHeld, "me too"))
	requireKind(t, err, ierr.ErrVersionConflict)
	require.Equal(t, types.ReminderStatusApproved, f.reminders.All(ctx)[0].ReminderStatus, "the first writer won")

	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
	require.NoError(t, err)
	racing.race = func() {
		_, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Body: lo.ToPtr("Their body")})
		require.NoError(t, err)
	}
	_, err = f.serviceWith(racing).UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("Mine")})
	requireKind(t, err, ierr.ErrVersionConflict)
	got, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, "Their body", got.Body)
	require.NotEqual(t, "Mine", got.Subject)
}

// racingReminderRepo runs a competing change once, right before the first write it receives.
type racingReminderRepo struct {
	collections.InvoiceReminderRepository
	race func()
}

func (r *racingReminderRepo) before() {
	if race := r.race; race != nil {
		r.race = nil
		race()
	}
}

func (r *racingReminderRepo) Create(ctx context.Context, d *collections.InvoiceReminder) error {
	r.before()
	return r.InvoiceReminderRepository.Create(ctx, d)
}

func (r *racingReminderRepo) CreateMany(ctx context.Context, d []*collections.InvoiceReminder) error {
	r.before()
	return r.InvoiceReminderRepository.CreateMany(ctx, d)
}

func (r *racingReminderRepo) Update(ctx context.Context, d, previous *collections.InvoiceReminder) error {
	r.before()
	return r.InvoiceReminderRepository.Update(ctx, d, previous)
}

func ptr(s string) *string { return &s }

// TestReminder_StatusMatrix tries every target status from every starting status through the one update call.
func TestReminder_StatusMatrix(t *testing.T) {
	pending, approved, held, skipped := types.ReminderStatusPending, types.ReminderStatusApproved, types.ReminderStatusHeld, types.ReminderStatusSkipped
	legal := map[types.ReminderStatus][]types.ReminderStatus{
		pending: {approved, held, skipped}, held: {pending, approved, skipped}, approved: {pending}, skipped: {pending},
	}
	for _, from := range []types.ReminderStatus{pending, held, approved, skipped} {
		for _, to := range []types.ReminderStatus{pending, approved, held, skipped} {
			t.Run(string(from)+"_to_"+string(to), func(t *testing.T) {
				ctx := reminderCtxFor("tenant_a", "env_a")
				f := newReminderFixture(t, ctx)
				f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
				f.overdue(t, ctx, "a1", "cust_1", 100, 5)
				if from != pending {
					_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(held, "wait"))
					require.NoError(t, err)
					if from != held {
						_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(from, ""))
						require.NoError(t, err)
					}
				}
				writes := f.reminders.Writes()
				got, err := f.svc.UpdateReminder(ctx, "a1", setStatus(to, "because"))
				if to == pending || to == approved {
					got, err = f.svc.UpdateReminder(ctx, "a1", setStatus(to, ""))
				}
				if !lo.Contains(legal[from], to) {
					requireKind(t, err, ierr.ErrVersionConflict)
					require.Equal(t, writes, f.reminders.Writes(), "an illegal move writes nothing")
					return
				}
				require.NoError(t, err)
				require.Equal(t, to, got.Status)
				require.Equal(t, to == approved, got.ApprovedAt != nil, "approved_at is set only when approved")
				if to == held || to == skipped {
					require.Equal(t, "because", got.StatusReason)
				} else {
					require.Empty(t, got.StatusReason)
				}
				require.Len(t, f.reminders.All(ctx), 1, "one row per invoice")
			})
		}
	}
}

func TestUpdateReminderRequest_Validate(t *testing.T) {
	for name, tc := range map[string]struct {
		req dto.UpdateReminderRequest
		ok  bool
	}{
		"empty":                  {dto.UpdateReminderRequest{}, false},
		"approve":                {dto.UpdateReminderRequest{Status: types.ReminderStatusApproved}, true},
		"hold without reason":    {dto.UpdateReminderRequest{Status: types.ReminderStatusHeld}, false},
		"hold with blank reason": {dto.UpdateReminderRequest{Status: types.ReminderStatusHeld, StatusReason: "  "}, false},
		"hold with reason":       {dto.UpdateReminderRequest{Status: types.ReminderStatusHeld, StatusReason: "wait"}, true},
		"skip without reason":    {dto.UpdateReminderRequest{Status: types.ReminderStatusSkipped}, true},
		"reason on approve":      {dto.UpdateReminderRequest{Status: types.ReminderStatusApproved, StatusReason: "x"}, false},
		"reason without status":  {dto.UpdateReminderRequest{StatusReason: "x"}, false},
		"unknown status":         {dto.UpdateReminderRequest{Status: "sent"}, false},
		"bad tone":               {dto.UpdateReminderRequest{Tone: ptr("angry")}, false},
		"blank subject":          {dto.UpdateReminderRequest{Subject: ptr(" ")}, false},
		"blank body":             {dto.UpdateReminderRequest{Body: ptr("")}, false},
		"approve with edit":      {dto.UpdateReminderRequest{Status: types.ReminderStatusApproved, Subject: ptr("s")}, true},
		"tone only":              {dto.UpdateReminderRequest{Tone: ptr("final")}, true},
		"reopen":                 {dto.UpdateReminderRequest{Status: types.ReminderStatusPending}, true},
	} {
		err := tc.req.Validate()
		if tc.ok {
			require.NoError(t, err, name)
		} else {
			requireKind(t, err, ierr.ErrValidation)
		}
	}
}

func TestReminder_ApproveWithEditsInOneRequest(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	before, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)

	got, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Status: types.ReminderStatusApproved, Tone: ptr("final"), Subject: ptr("Mine")})
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusApproved, got.Status)
	require.Equal(t, types.ReminderToneFinal, got.Tone)
	require.Equal(t, "Mine", got.Subject)
	require.True(t, got.Edited)
	require.Equal(t, before.Subject, got.OriginalSubject, "original is frozen at the first action")
	_, err = f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Body: ptr("x")})
	requireKind(t, err, ierr.ErrVersionConflict)
}

func TestReminder_BulkUpdate(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	for _, id := range []string{"i1", "i2", "i3", "i4", "i5"} {
		f.overdue(t, ctx, id, "cust_1", 100, 5)
	}
	_, err := f.svc.UpdateReminder(ctx, "i2", setStatus(types.ReminderStatusHeld, "wait"))
	require.NoError(t, err)
	_, err = f.svc.UpdateReminder(ctx, "i3", setStatus(types.ReminderStatusSkipped, ""))
	require.NoError(t, err)
	_, err = f.svc.UpdateReminder(ctx, "i4", &dto.UpdateReminderRequest{Body: ptr("Custom")})
	require.NoError(t, err)
	writes := f.reminders.Writes()

	for name, req := range map[string]*dto.BulkUpdateRemindersRequest{
		"no ids":         {UpdateReminderRequest: *setStatus(types.ReminderStatusApproved, "")},
		"too many ids":   bulkStatus(types.ReminderStatusApproved, make([]string, dto.RemindersMaxBulk+1)),
		"nothing to do":  {InvoiceIDs: []string{"i1"}},
		"hold no reason": bulkStatus(types.ReminderStatusHeld, []string{"i1"}),
		"subject":        {InvoiceIDs: []string{"i1"}, UpdateReminderRequest: dto.UpdateReminderRequest{Status: types.ReminderStatusApproved, Subject: ptr("s")}},
		"body":           {InvoiceIDs: []string{"i1"}, UpdateReminderRequest: dto.UpdateReminderRequest{Body: ptr("b")}},
	} {
		_, err := f.svc.BulkUpdateReminders(ctx, req)
		requireKind(t, err, ierr.ErrValidation)
		_ = name
	}
	_, err = f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"i1", "unknown"}))
	requireKind(t, err, ierr.ErrNotFound)
	f.pay(t, ctx, "i5")
	_, err = f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"i1", "i5"}))
	requireKind(t, err, ierr.ErrNotFound)
	require.Equal(t, writes, f.reminders.Writes(), "a failed bulk request writes nothing")

	_, err = f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"i3", "i4", "i1"}))
	requireKind(t, err, ierr.ErrVersionConflict)
	require.Equal(t, writes, f.reminders.Writes(), "a skipped reminder cannot be approved, so the whole request fails")

	out, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"i4", "i1", "i2", "i1"}))
	require.NoError(t, err)
	require.Equal(t, []string{"i4", "i1", "i2"}, lo.Map(out.Drafts, func(d dto.InvoiceReminderResponse, _ int) string { return d.ID }),
		"a held reminder can be approved; duplicates count once; the response keeps the request order")
	for _, d := range out.Drafts {
		require.Equal(t, types.ReminderStatusApproved, d.Status)
		require.NotNil(t, d.ApprovedAt)
	}
	i4, _ := lo.Find(out.Drafts, func(d dto.InvoiceReminderResponse) bool { return d.ID == "i4" })
	require.Equal(t, "Custom", i4.Body, "an edited reminder keeps its edit when approved in bulk")
	require.Len(t, f.reminders.All(ctx), 4)

	_, err = f.svc.BulkUpdateReminders(ctx, &dto.BulkUpdateRemindersRequest{InvoiceIDs: []string{"i1", "i4"}, UpdateReminderRequest: *setStatus(types.ReminderStatusHeld, "wait")})
	requireKind(t, err, ierr.ErrVersionConflict)

	reopened, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusPending, []string{"i1", "i4", "i2"}))
	require.NoError(t, err)
	require.Len(t, reopened.Drafts, 3)
	toned, err := f.svc.BulkUpdateReminders(ctx, &dto.BulkUpdateRemindersRequest{InvoiceIDs: []string{"i1"}, UpdateReminderRequest: dto.UpdateReminderRequest{Tone: ptr("final")}})
	require.NoError(t, err)
	require.Equal(t, types.ReminderToneFinal, toned.Drafts[0].Tone)
}

func TestReminder_BulkUpdateFailsWhenAnInvoiceChangedMeanwhile(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	f.overdue(t, ctx, "a2", "cust_1", 100, 5)
	f.overdue(t, ctx, "a3", "cust_1", 100, 5)
	_, err := f.svc.UpdateReminder(ctx, "a3", &dto.UpdateReminderRequest{Body: ptr("Custom")})
	require.NoError(t, err)

	racing := &racingReminderRepo{InvoiceReminderRepository: f.reminders}
	racing.race = func() {
		_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusHeld, "someone else"))
		require.NoError(t, err)
	}
	_, err = f.serviceWith(racing).BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a1", "a2"}))
	requireKind(t, err, ierr.ErrVersionConflict)
	held, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusHeld, held.Status, "the other writer won")
	untouched, err := f.svc.Get(ctx, "a2")
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, untouched.Status, "one conflict stops the whole request")
	require.Nil(t, untouched.CreatedAt)

	racing.race = func() {
		_, err := f.svc.UpdateReminder(ctx, "a3", &dto.UpdateReminderRequest{Body: ptr("Theirs")})
		require.NoError(t, err)
	}
	_, err = f.serviceWith(racing).BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a3"}))
	requireKind(t, err, ierr.ErrVersionConflict)
}

func TestReminder_BulkUpdateFailsWhenAnotherRequestCreatedAReminder(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	f.overdue(t, ctx, "a2", "cust_1", 100, 5)

	racing := &racingReminderRepo{InvoiceReminderRepository: f.reminders}
	racing.race = func() {
		_, err := f.svc.UpdateReminder(ctx, "a2", setStatus(types.ReminderStatusSkipped, ""))
		require.NoError(t, err)
	}
	_, err := f.serviceWith(racing).BulkUpdateReminders(ctx, &dto.BulkUpdateRemindersRequest{InvoiceIDs: []string{"a1", "a2"}, UpdateReminderRequest: *setStatus(types.ReminderStatusHeld, "wait")})
	requireKind(t, err, ierr.ErrVersionConflict)

	all := f.reminders.All(ctx)
	require.Len(t, all, 1, "the reminder for a1 is not inserted either")
	require.Equal(t, types.ReminderStatusSkipped, all[0].ReminderStatus)
}

func TestReminder_PaidInvoiceHidesItsReminderAndTenantsAreIsolated(t *testing.T) {
	ctxA := reminderCtxFor("tenant_a", "env_a")
	ctxB := reminderCtxFor("tenant_b", "env_a")
	f := newReminderFixture(t, ctxA)
	f.customer(t, ctxA, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctxA, "a1", "cust_1", 100, 5)
	f.setCollections(t, ctxB, true)
	f.setCollections(t, reminderCtxFor("tenant_a", "env_b"), true)
	_, err := f.svc.UpdateReminder(ctxA, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)

	_, err = f.svc.Get(ctxB, "a1")
	requireKind(t, err, ierr.ErrNotFound)
	_, err = f.svc.UpdateReminder(reminderCtxFor("tenant_a", "env_b"), "a1", setStatus(types.ReminderStatusApproved, ""))
	requireKind(t, err, ierr.ErrNotFound)

	f.pay(t, ctxA, "a1")
	_, err = f.svc.Get(ctxA, "a1")
	requireKind(t, err, ierr.ErrNotFound)
	_, err = f.svc.UpdateReminder(ctxA, "a1", setStatus(types.ReminderStatusPending, ""))
	requireKind(t, err, ierr.ErrNotFound)
}

func TestReminder_ListUsesTheCollectionsRepositoryAndDoesNotWrite(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.example")
	due := clock.Now().Add(-3 * day)
	listed := collections.NewInvoiceWithReminderBuilder(&collections.InvoiceWithReminder{InvoiceID: "a1"}).WithInvoice("INV-1", "cust_1", "usd").
		WithAmounts(decimal.NewFromInt(100), decimal.NewFromInt(100)).WithDates(&due, nil).Build()
	f.board.rows = []collections.InvoiceWithReminder{*listed}
	f.board.hasMore = true
	f.board.summary = *collections.NewReminderSummaryBuilder(nil).
		WithTotals([]collections.CurrencyTotal{{Currency: "usd", Amount: decimal.NewFromInt(100)}}).
		WithCounts(4, 2).
		WithStatuses(map[types.ReminderStatus]int{types.ReminderStatusPending: 3, types.ReminderStatusApproved: 1}).
		Build()

	resp, err := f.svc.List(ctx, &dto.ListRemindersRequest{Status: "pending", Q: " acme ", Currency: "USD", CustomerID: "cust_1", Limit: 10, Page: 3})
	require.NoError(t, err)
	require.Equal(t, 20, f.board.offset)
	require.Equal(t, 10, f.board.limit)
	require.Equal(t, []types.ReminderStatus{types.ReminderStatusPending}, f.board.filter.Statuses)
	require.Equal(t, "cust_1", f.board.filter.CustomerID)
	require.Equal(t, "acme", f.board.query.Text)
	require.Equal(t, "usd", f.board.query.Currency)
	require.True(t, resp.HasMore)
	require.Equal(t, 3, resp.Page)
	require.Len(t, resp.Drafts, 1)
	require.Equal(t, "ap@acme.example", resp.Drafts[0].CustomerEmail)
	require.Equal(t, 3, resp.Drafts[0].DaysOverdue)
	require.Equal(t, dto.ReminderSummary{
		OverdueTotals: []dto.CollectionsCurrencyTotal{{Currency: "usd", Amount: decimal.NewFromInt(100)}},
		InvoiceCount:  4, CustomerCount: 2, ToReview: 3, Approved: 1,
	}, resp.Summary)
	require.Zero(t, f.reminders.Writes())

	for name, req := range map[string]dto.ListRemindersRequest{
		"limit above the maximum": {Limit: 101}, "negative page": {Page: -1}, "too deep": {Page: 51, Limit: 100},
		"bad status": {Status: "nope"}, "page that would overflow": {Page: math.MaxInt, Limit: 100},
		"as_of too old":       {AsOf: lo.ToPtr(time.Now().Add(-25 * time.Hour))},
		"as_of in the future": {AsOf: lo.ToPtr(time.Now().Add(time.Hour))},
	} {
		_, err := f.svc.List(ctx, &req)
		requireKind(t, err, ierr.ErrValidation)
		_ = name
	}
	_, err = f.svc.List(ctx, &dto.ListRemindersRequest{Page: 50, Limit: 100})
	require.NoError(t, err, "the deepest allowed page")
	defaults, err := f.svc.List(ctx, &dto.ListRemindersRequest{})
	require.NoError(t, err)
	require.Equal(t, 15, f.board.limit, "the list defaults to 15")
	require.Equal(t, 1, defaults.Page)
	asOf := time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
	pinned, err := f.svc.List(ctx, &dto.ListRemindersRequest{AsOf: &asOf})
	require.NoError(t, err)
	require.True(t, pinned.AsOf.Equal(asOf))
	require.True(t, f.board.query.AsOf.Equal(asOf))
}

func TestReminder_ListedRowsShowStoredMailOnlyWhenEditedOrApproved(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	due := clock.Now().Add(-3 * day)
	row := func(id string, rem *collections.InvoiceReminder) collections.InvoiceWithReminder {
		return *collections.NewInvoiceWithReminderBuilder(&collections.InvoiceWithReminder{InvoiceID: id}).WithInvoice(id, "cust_1", "usd").
			WithAmounts(decimal.Zero, decimal.NewFromInt(10)).WithDates(&due, nil).WithReminder(rem).Build()
	}
	stored := func(status types.ReminderStatus, mailBody string) *collections.InvoiceReminder {
		return collections.NewInvoiceReminderBuilder(&collections.InvoiceReminder{ID: "irem_x"}).
			WithStatus(status, "", nil).
			WithOriginal(types.ReminderToneFriendly, "orig", "orig body").
			WithText(types.ReminderToneFriendly, "orig", mailBody).
			Build()
	}
	f.board.rows = []collections.InvoiceWithReminder{
		row("untouched_held", stored(types.ReminderStatusHeld, "orig body")),
		row("edited_held", stored(types.ReminderStatusHeld, "my words")),
		row("approved", stored(types.ReminderStatusApproved, "orig body")),
	}
	resp, err := f.svc.List(ctx, &dto.ListRemindersRequest{})
	require.NoError(t, err)
	byID := lo.KeyBy(resp.Drafts, func(d dto.InvoiceReminderResponse) string { return d.ID })
	require.Contains(t, byID["untouched_held"].Body, "Hi Acme", "an untouched held reminder is rendered fresh")
	require.False(t, byID["untouched_held"].Edited)
	require.Equal(t, "my words", byID["edited_held"].Body)
	require.True(t, byID["edited_held"].Edited)
	require.Equal(t, "orig body", byID["edited_held"].OriginalBody)
	require.Equal(t, "orig body", byID["approved"].Body, "an approved reminder shows exactly what was approved")
}

func TestReminder_RowsOfManyInvoicesStayDistinct(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	for i := 0; i < 5; i++ {
		f.overdue(t, ctx, fmt.Sprintf("a%d", i), "cust_1", 100, 5)
	}
	_, err := f.svc.UpdateReminder(ctx, "a3", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	got, err := f.svc.Get(ctx, "a2")
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, got.Status, "one reminder per invoice: a sibling invoice is untouched")
	require.Len(t, f.reminders.All(ctx), 1)
}

func TestReminder_ApprovalChecksThePreviewedText(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	f.overdue(t, ctx, "a2", "cust_1", 100, 5)

	preview, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	approved := setStatus(types.ReminderStatusApproved, "")
	approved.ExpectedSubject, approved.ExpectedBody = &preview.Subject, &preview.Body
	got, err := f.svc.UpdateReminder(ctx, "a1", approved)
	require.NoError(t, err, "the text the reviewer saw is the text approved")
	require.Equal(t, preview.Body, got.Body)

	stale := setStatus(types.ReminderStatusApproved, "")
	stale.ExpectedBody = lo.ToPtr("What the reviewer saw when the invoice was 3 days overdue")
	_, err = f.svc.UpdateReminder(ctx, "a2", stale)
	requireKind(t, err, ierr.ErrVersionConflict)
	row, err := f.svc.Get(ctx, "a2")
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, row.Status, "a stale preview approves nothing")

	for name, bad := range map[string]*dto.UpdateReminderRequest{
		"expected text without an approval": {Status: types.ReminderStatusHeld, StatusReason: "x", ExpectedBody: lo.ToPtr("b")},
		"expected text on a mail edit":      {Body: lo.ToPtr("b"), ExpectedSubject: lo.ToPtr("s")},
	} {
		require.Error(t, bad.Validate(), name)
	}
	bulk := &dto.BulkUpdateRemindersRequest{InvoiceIDs: []string{"a1"}, UpdateReminderRequest: *approved}
	require.Error(t, bulk.Validate(), "bulk approval cannot check the text of one reminder")
}

func TestToneReason(t *testing.T) {
	tests := []struct {
		name string
		view dto.InvoiceReminderResponse
		days int
		want string
	}{
		{"pending by age", dto.InvoiceReminderResponse{Status: types.ReminderStatusPending, Tone: types.ReminderToneFriendly}, 12, "Invoice is 12 days overdue, so a friendly tone."},
		{"held by age", dto.InvoiceReminderResponse{Status: types.ReminderStatusHeld, Tone: types.ReminderToneFirm}, 40, "Invoice is 40 days overdue, so a firm tone."},
		{"overdue today", dto.InvoiceReminderResponse{Status: types.ReminderStatusPending, Tone: types.ReminderToneFriendly}, 0, "Invoice became overdue today, so a friendly tone."},
		{"one day", dto.InvoiceReminderResponse{Status: types.ReminderStatusPending, Tone: types.ReminderToneFriendly}, 1, "Invoice is 1 day overdue, so a friendly tone."},
		{"edited text, same tone", dto.InvoiceReminderResponse{Status: types.ReminderStatusPending, Tone: types.ReminderToneFriendly, Edited: true, OriginalTone: types.ReminderToneFriendly}, 12, "Invoice is 12 days overdue, so a friendly tone."},
		{"tone set by the person", dto.InvoiceReminderResponse{Status: types.ReminderStatusPending, Tone: types.ReminderToneFirm, Edited: true, OriginalTone: types.ReminderToneFriendly}, 12, "Tone set to firm by you (invoice is 12 days overdue)."},
		{"approved keeps its tone", dto.InvoiceReminderResponse{Status: types.ReminderStatusApproved, Tone: types.ReminderToneFirm}, 12, "Approved with a firm tone, kept as approved (12 days overdue)."},
		{"approved, now much older", dto.InvoiceReminderResponse{Status: types.ReminderStatusApproved, Tone: types.ReminderToneFriendly}, 75, "Approved with a friendly tone, kept as approved (75 days overdue)."},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, toneReason(tt.view, tt.days), tt.name)
	}
}

func TestReminderText_ValuesTypedByPeopleStayOnOneLine(t *testing.T) {
	inv := reminderInvoice{number: "INV-1\r\nBcc: attacker@example.com", currency: "usd", remaining: decimal.NewFromInt(100), due: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
	subject, body, err := renderReminder(types.ReminderToneFirm, "Acme\nCorp\x00", "Flex\tBilling\r\n", inv, time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	require.NotContains(t, subject, "\n")
	require.NotContains(t, subject, "\r")
	require.Equal(t, "Reminder: Invoice INV-1 Bcc: attacker@example.com is past due: $100", subject)
	require.Contains(t, body, "Hi Acme Corp,\n")
	require.Contains(t, body, "Flex Billing Billing\n")
	require.NotContains(t, body, "\x00")
	for _, line := range strings.Split(body, "\n") {
		require.False(t, strings.HasPrefix(strings.TrimSpace(line), "Bcc:"), "no value can start a line of its own")
	}
}

func TestReminder_EditedSubjectStaysOnOneLine(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)

	got, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("Pay now\r\nBcc: x@example.com")})
	require.NoError(t, err)
	require.Equal(t, "Pay now Bcc: x@example.com", got.Subject)
}
