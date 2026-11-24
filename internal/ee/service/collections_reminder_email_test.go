package service

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// emailSetup says which of the gates in front of a reminder email are open.
type emailSetup struct {
	provider, enabled, attachPDF bool
	replyTo                      string
}

var emailsOn = emailSetup{provider: true, enabled: true, replyTo: "billing@flex.test"}

// withEmail opens or closes the gates in front of the reminder email and rebuilds the service.
func (f *reminderFixture) withEmail(t *testing.T, ctx context.Context, o emailSetup) {
	t.Helper()
	f.emailOn = o.provider
	require.NoError(t, f.tenants.Update(ctx, &tenant.Tenant{ID: types.GetTenantID(ctx), Name: "Flex", BillingDetails: tenant.TenantBillingDetails{Email: o.replyTo}}))
	require.NoError(t, f.settings.Create(ctx, &settings.Setting{
		ID: types.GenerateUUIDWithPrefix("setting"), Key: types.SettingKeyEmailNotificationConfig, Value: map[string]interface{}{"enabled": o.enabled, "attach_invoice_pdf": o.attachPDF},
		EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
	}))
	f.svc = f.serviceWith(f.reminders)
}

func (f *reminderFixture) emails(t *testing.T, ctx context.Context, invoiceID string) []*emaillog.EmailLog {
	t.Helper()
	filter := types.NewEmailLogFilter()
	filter.EntityIDs = []string{invoiceID}
	logs, err := f.logs.List(ctx, filter)
	require.NoError(t, err)
	return logs
}

func emailFixture(t *testing.T, o emailSetup) (*reminderFixture, context.Context) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.customer(t, ctx, "cust_noemail", "No Mail", "")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	f.withEmail(t, ctx, o)
	return f, ctx
}

func TestReminderEmail_ApproveSendsTheFrozenTextAtOnce(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("Custom subject"), Body: lo.ToPtr("Custom body")})
	require.NoError(t, err)
	require.Empty(t, f.emails(t, ctx, "a1"), "editing a reminder sends nothing")

	approved, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	logs := f.emails(t, ctx, "a1")
	require.Len(t, logs, 1)
	e := logs[0]
	require.Equal(t, types.EmailTemplateInvoicePaymentReminder, e.TemplateKey)
	require.Equal(t, types.EmailTriggerManual, e.Trigger)
	require.Equal(t, types.SystemEntityTypeInvoice, e.EntityType)
	require.Equal(t, "cust_1", e.CustomerID)
	require.Equal(t, []string{"ap@acme.test"}, e.ToAddresses)
	require.Equal(t, "billing@flex.test", e.ReplyTo)
	require.Equal(t, types.EmailDeliveryStatusSent, e.DeliveryStatus, "the email goes out as part of the approval")
	require.Equal(t, "Custom subject", e.Subject)
	require.Equal(t, "Custom body", e.Metadata[emailMetaReminderBody])
	require.Equal(t, reminderEmailKey("a1", f.reminders.All(ctx)[0].ID, *approved.ApprovedAt), lo.FromPtr(e.IdempotencyKey))

	sent := f.sent.all()
	require.Len(t, sent, 1)
	require.Equal(t, []string{"ap@acme.test"}, sent[0].To)
	require.Equal(t, "Custom subject", sent[0].Subject)
	require.Contains(t, sent[0].Text, "Custom body")

	require.NotNil(t, approved.Email)
	require.Equal(t, dto.ReminderEmailStatus{Status: types.EmailDeliveryStatusSent, To: "ap@acme.test", SentAt: e.SentAt}, *approved.Email)

	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	requireKind(t, err, ierr.ErrVersionConflict)
	_, err = f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Body: lo.ToPtr("Changed after approval")})
	requireKind(t, err, ierr.ErrVersionConflict)
	require.Len(t, f.emails(t, ctx, "a1"), 1, "a repeated approval makes no second email")
	require.Len(t, f.sent.all(), 1, "and sends nothing again")

	got, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, approved.Email, got.Email)
}

func TestReminderEmail_ProviderOutageLeavesTheEmailQueuedForTheDispatcher(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	f.sent.down = true
	approved, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err, "approving still works")
	require.Equal(t, types.ReminderStatusApproved, approved.Status)
	require.NotNil(t, approved.Email)
	require.Equal(t, types.EmailDeliveryStatusQueued, approved.Email.Status)
	require.Equal(t, "ap@acme.test", approved.Email.To)

	f.sent.down = false
	require.NoError(t, NewEmailNotificationService(f.params(f.reminders)).DeliverEmail(ctx, f.emails(t, ctx, "a1")[0].ID, false), "the dispatcher sends it later")
	require.Len(t, f.sent.all(), 1)
}

func TestReminderEmail_TestRecipientReplacesTheCustomer(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.redirect = "Tester@Flexprice.test"
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	f.withEmail(t, ctx, emailsOn)

	approved, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	e := f.emails(t, ctx, "a1")[0]
	require.Equal(t, []string{"tester@flexprice.test"}, e.ToAddresses)
	require.Empty(t, e.CCAddresses)
	require.Equal(t, "ap@acme.test", e.Metadata[emailMetaOriginalTo])
	require.Equal(t, []string{"tester@flexprice.test"}, f.sent.all()[0].To, "the provider only ever sees the test recipient")
	require.Equal(t, "tester@flexprice.test", approved.Email.To, "the response shows who it really went to")
}

func TestReminderEmail_TestRecipientDoesNotInventARecipient(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.redirect = "tester@flexprice.test"
	f.customer(t, ctx, "cust_noemail", "No Mail", "")
	f.overdue(t, ctx, "a2", "cust_noemail", 100, 5)
	f.withEmail(t, ctx, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a2", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Equal(t, types.EmailDeliveryStatusSkipped, f.emails(t, ctx, "a2")[0].DeliveryStatus)
	require.Empty(t, f.sent.all())
}

func TestReminderEmail_ReopenCancelsAQueuedEmailAndApprovingAgainMakesANewOne(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	f.sent.down = true
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)

	reopened, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, reopened.Status)
	require.Nil(t, reopened.Email, "a reminder that is not approved carries no email")
	logs := f.emails(t, ctx, "a1")
	require.Len(t, logs, 1)
	require.Equal(t, types.EmailDeliveryStatusSkipped, logs[0].DeliveryStatus)
	require.Contains(t, lo.FromPtr(logs[0].StatusReason), "reopened")

	f.sent.down = false
	_, err = f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Body: lo.ToPtr("Second round")})
	require.NoError(t, err)
	second, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	statuses := lo.Map(f.emails(t, ctx, "a1"), func(e *emaillog.EmailLog, _ int) types.EmailDeliveryStatus { return e.DeliveryStatus })
	require.ElementsMatch(t, []types.EmailDeliveryStatus{types.EmailDeliveryStatusSkipped, types.EmailDeliveryStatusSent}, statuses)
	require.Equal(t, types.EmailDeliveryStatusSent, second.Email.Status, "the response shows the new email, not the cancelled one")
}

func TestReminderEmail_ReopenAfterTheEmailWentOutIsRefused(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)

	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
	requireKind(t, err, ierr.ErrVersionConflict)
	got, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusApproved, got.Status, "the reminder stays approved")
}

func TestReminderEmail_CustomerWithoutAddressIsRecordedAsSkipped(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	f.overdue(t, ctx, "a2", "cust_noemail", 100, 5)
	_, err := f.svc.UpdateReminder(ctx, "a2", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	logs := f.emails(t, ctx, "a2")
	require.Len(t, logs, 1)
	require.Equal(t, types.EmailDeliveryStatusSkipped, logs[0].DeliveryStatus)
	require.Equal(t, "Customer has no email address", lo.FromPtr(logs[0].StatusReason))
}

func TestReminderEmail_ApprovingStillWorksWhenEmailsAreOff(t *testing.T) {
	for name, setup := range map[string]emailSetup{
		"provider not configured":      {provider: false, enabled: true, replyTo: "billing@flex.test"},
		"disabled for the environment": {provider: true, enabled: false, replyTo: "billing@flex.test"},
		"tenant has no reply-to email": {provider: true, enabled: true},
	} {
		t.Run(name, func(t *testing.T) {
			f, ctx := emailFixture(t, setup)
			got, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
			require.NoError(t, err)
			require.Equal(t, types.ReminderStatusApproved, got.Status)
			require.Empty(t, f.emails(t, ctx, "a1"), "no email is recorded")
			_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
			require.NoError(t, err, "and reopening has nothing to cancel")
		})
	}
}

func TestReminderEmail_BulkApproveSendsOneEmailPerInvoice(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	f.overdue(t, ctx, "a2", "cust_1", 100, 5)
	f.overdue(t, ctx, "a3", "cust_1", 100, 5)
	_, err := f.svc.UpdateReminder(ctx, "a3", setStatus(types.ReminderStatusHeld, "wait"))
	require.NoError(t, err)

	out, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a1", "a2", "a3"}))
	require.NoError(t, err)
	require.Len(t, out.Drafts, 3)
	for _, d := range out.Drafts {
		require.NotNil(t, d.Email, d.ID)
		require.Equal(t, types.EmailDeliveryStatusSent, d.Email.Status, d.ID)
	}
	require.Len(t, f.sent.all(), 3)
	again, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a1", "a2", "a3"}))
	require.NoError(t, err)
	require.Empty(t, again.Drafts, "approved reminders are left alone")
	require.Len(t, f.sent.all(), 3, "no duplicate sends")

	back, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusPending, []string{"a1", "a2", "a3"}))
	require.NoError(t, err)
	require.Empty(t, back.Drafts, "sent emails cannot be taken back")
}

func TestReminderEmail_BulkApproveKeepsUnsentEmailsQueuedAndReopenCancelsThem(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	f.sent.down = true
	f.overdue(t, ctx, "a2", "cust_1", 100, 5)
	out, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a1", "a2"}))
	require.NoError(t, err)
	for _, d := range out.Drafts {
		require.Equal(t, types.EmailDeliveryStatusQueued, d.Email.Status, d.ID)
	}
	back, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusPending, []string{"a1", "a2"}))
	require.NoError(t, err)
	require.Len(t, back.Drafts, 2)
	for _, id := range []string{"a1", "a2"} {
		require.Equal(t, types.EmailDeliveryStatusSkipped, f.emails(t, ctx, id)[0].DeliveryStatus, id)
	}
}

func TestReminderEmail_ListCarriesTheEmailOfApprovedRemindersInOneQuery(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	for _, id := range []string{"a2", "a3", "a4"} {
		f.overdue(t, ctx, id, "cust_1", 100, 5)
	}
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	f.sent.down = true
	_, err = f.svc.UpdateReminder(ctx, "a2", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	f.sent.down = false
	_, err = f.svc.UpdateReminder(ctx, "a3", setStatus(types.ReminderStatusHeld, "wait"))
	require.NoError(t, err)

	rows, err := f.reminders.GetRows(ctx, clock.Now(), []string{"a1", "a2", "a3", "a4"})
	require.NoError(t, err)
	f.board.rows = rows
	f.lists = 0
	resp, err := f.svc.List(ctx, &dto.ListRemindersRequest{})
	require.NoError(t, err)
	require.Equal(t, 1, f.lists, "one email query for the whole page")
	byID := lo.KeyBy(resp.Drafts, func(d dto.InvoiceReminderResponse) string { return d.ID })
	require.Equal(t, types.EmailDeliveryStatusSent, byID["a1"].Email.Status)
	require.Equal(t, types.EmailDeliveryStatusQueued, byID["a2"].Email.Status)
	require.Nil(t, byID["a3"].Email, "a held reminder has no email")
	require.Nil(t, byID["a4"].Email, "neither has a reminder nobody acted on")
}

func TestReminderEmail_SendWaitIsLongerForEmailsWithAnAttachment(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	wait := func(f *reminderFixture) time.Duration { return f.svc.(*reminderService).sendWait(ctx) }
	setting := func(f *reminderFixture, attachPDF bool) {
		require.NoError(t, f.settings.Create(ctx, &settings.Setting{
			ID: types.GenerateUUIDWithPrefix("setting"), Key: types.SettingKeyEmailNotificationConfig,
			Value:         map[string]interface{}{"enabled": true, "attach_invoice_pdf": attachPDF},
			EnvironmentID: types.GetEnvironmentID(ctx), BaseModel: types.GetDefaultBaseModel(ctx),
		}))
	}

	plain := newReminderFixture(t, ctx)
	setting(plain, false)
	require.Equal(t, reminderSendWait, wait(plain), "no attachment: the short bound")

	withPDF := newReminderFixture(t, ctx)
	setting(withPDF, true)
	require.Equal(t, reminderSendWaitWithAttachment, wait(withPDF), "the invoice PDF is attached")
}

func TestReminderEmail_PDFThatCannotBeProducedNeverHoldsTheEmail(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	f.withEmail(t, ctx, emailSetup{provider: true, enabled: true, attachPDF: true, replyTo: "billing@flex.test"})

	approved, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Equal(t, types.EmailDeliveryStatusSent, approved.Email.Status, "no PDF, and the email still goes out at once")
	require.Len(t, f.sent.all(), 1)
	require.Empty(t, f.sent.all()[0].Attachments)
	e := f.emails(t, ctx, "a1")[0]
	require.Equal(t, emailPDFUnavailable, e.Metadata[emailMetaPDF])
	for _, queued := range f.queued(t, ctx) {
		require.NotEqual(t, e.ID, queued.ID, "nothing is left queued for the dispatcher")
	}
}

func TestReminderEmail_PDFLinkStillAccompaniesTheEmail(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.invoice(t, ctx, "a1", func(i *invoice.Invoice) {
		due := clock.Now().Add(-5 * day)
		i.CustomerID, i.DueDate, i.InvoicePDFURL = "cust_1", &due, lo.ToPtr("https://files.test/a1.pdf")
	})
	f.withEmail(t, ctx, emailSetup{provider: true, enabled: true, attachPDF: true, replyTo: "billing@flex.test"})

	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	e := f.emails(t, ctx, "a1")[0]
	require.Equal(t, types.EmailDeliveryStatusSent, e.DeliveryStatus)
	require.Equal(t, emailPDFInvoiceLink, e.Metadata[emailMetaPDF], "a PDF that can be linked is still linked")
	require.Contains(t, f.sent.all()[0].Text, "https://files.test/a1.pdf")
}

func TestReminderEmail_WithoutThePDFSettingNothingIsRecordedAboutIt(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Equal(t, emailPDFDisabled, f.emails(t, ctx, "a1")[0].Metadata[emailMetaPDF])
}

// queued lists the emails still waiting for the dispatcher.
func (f *reminderFixture) queued(t *testing.T, ctx context.Context) []*emaillog.EmailLog {
	t.Helper()
	filter := types.NewEmailLogFilter()
	filter.DeliveryStatuses = []types.EmailDeliveryStatus{types.EmailDeliveryStatusQueued}
	logs, err := f.logs.List(ctx, filter)
	require.NoError(t, err)
	return logs
}
