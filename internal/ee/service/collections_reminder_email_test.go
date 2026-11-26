package service

import (
	"context"
	"testing"

	"github.com/cockroachdb/errors"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/domain/invoice"
	"github.com/flexprice/flexprice/internal/domain/settings"
	"github.com/flexprice/flexprice/internal/domain/tenant"
	"github.com/flexprice/flexprice/internal/email/emailtypes"
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
	_ = f.settings.DeleteByKey(ctx, types.SettingKeyEmailNotificationConfig)
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

// dispatch runs the email dispatcher once, as the Temporal cron does.
func (f *reminderFixture) dispatch(t *testing.T, ctx context.Context) *EmailDispatchResult {
	t.Helper()
	res, err := NewEmailNotificationService(f.params(f.reminders)).DispatchEmails(ctx)
	require.NoError(t, err)
	return res
}

func TestReminderEmail_ApproveOnlyQueuesTheFrozenText(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Subject: lo.ToPtr("Custom subject"), Body: lo.ToPtr("Custom body")})
	require.NoError(t, err)
	require.Empty(t, f.emails(t, ctx, "a1"), "editing a reminder sends nothing")

	approved, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Empty(t, f.sent.all(), "approving never calls the provider")
	logs := f.emails(t, ctx, "a1")
	require.Len(t, logs, 1)
	e := logs[0]
	require.Equal(t, types.EmailTemplateInvoicePaymentReminder, e.TemplateKey)
	require.Equal(t, types.EmailTriggerManual, e.Trigger)
	require.Equal(t, types.SystemEntityTypeInvoice, e.EntityType)
	require.Equal(t, "cust_1", e.CustomerID)
	require.Equal(t, []string{"ap@acme.test"}, e.ToAddresses)
	require.Equal(t, "billing@flex.test", e.ReplyTo)
	require.Equal(t, types.EmailDeliveryStatusQueued, e.DeliveryStatus)
	require.Equal(t, "Custom subject", e.Subject)
	require.Equal(t, "Custom body", e.Metadata[emailMetaReminderBody])
	require.Equal(t, reminderEmailKey("a1", f.reminders.All(ctx)[0].ID, *approved.ApprovedAt), lo.FromPtr(e.IdempotencyKey))
	require.Equal(t, &dto.ReminderEmailStatus{Status: types.EmailDeliveryStatusQueued, To: []string{"ap@acme.test"}}, approved.Email)

	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	requireKind(t, err, ierr.ErrVersionConflict)
	_, err = f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Body: lo.ToPtr("Changed after approval")})
	requireKind(t, err, ierr.ErrVersionConflict)
	require.Len(t, f.emails(t, ctx, "a1"), 1, "a repeated approval makes no second email")
	require.Empty(t, f.sent.all())

	res := f.dispatch(t, ctx)
	require.Equal(t, 1, res.Sent, "the dispatcher sends it")
	sent := f.sent.all()
	require.Len(t, sent, 1)
	require.Equal(t, []string{"ap@acme.test"}, sent[0].To)
	require.Equal(t, "Custom subject", sent[0].Subject)
	require.Contains(t, sent[0].Text, "Custom body")
	got, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, types.EmailDeliveryStatusSent, got.Email.Status, "the reminder shows what the dispatcher did")
	require.NotNil(t, got.Email.SentAt)
	require.Equal(t, 0, f.dispatch(t, ctx).Sent, "and sends it once")
}

func TestReminderEmail_ProviderOutageKeepsTheEmailQueuedForTheNextRun(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)

	f.sent.down = true
	require.Equal(t, 1, f.dispatch(t, ctx).Deferred)
	got, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, types.EmailDeliveryStatusQueued, got.Email.Status)

	f.sent.down = false
	require.Equal(t, 1, f.dispatch(t, ctx).Sent)
}

func TestReminderEmail_ReopenCancelsAQueuedEmailAndApprovingAgainMakesANewOne(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
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
	require.Equal(t, 0, f.dispatch(t, ctx).Sent, "a cancelled email is never sent")

	_, err = f.svc.UpdateReminder(ctx, "a1", &dto.UpdateReminderRequest{Body: lo.ToPtr("Second round")})
	require.NoError(t, err)
	second, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	statuses := lo.Map(f.emails(t, ctx, "a1"), func(e *emaillog.EmailLog, _ int) types.EmailDeliveryStatus { return e.DeliveryStatus })
	require.ElementsMatch(t, []types.EmailDeliveryStatus{types.EmailDeliveryStatusSkipped, types.EmailDeliveryStatusQueued}, statuses)
	require.Equal(t, types.EmailDeliveryStatusQueued, second.Email.Status, "the response shows the new email, not the cancelled one")
}

func TestReminderEmail_ReopenAfterTheEmailWentOutIsRefused(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	require.Equal(t, 1, f.dispatch(t, ctx).Sent)

	_, err = f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusPending, ""))
	requireKind(t, err, ierr.ErrVersionConflict)
	got, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusApproved, got.Status, "the reminder stays approved")
}

func TestReminderEmail_DeliveryEventsReachTheReminder(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	f.dispatch(t, ctx)

	e := f.emails(t, ctx, "a1")[0]
	require.NoError(t, f.logs.Update(ctx, applyDeliveryEvent(e, emailtypes.DeliveryEvent{
		ProviderMessageID: lo.FromPtr(e.ProviderMessageID), Status: types.EmailDeliveryStatusBounced, Reason: "mailbox full", OccurredAt: clock.Now(),
	})))
	got, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, types.EmailDeliveryStatusBounced, got.Email.Status, "a webhook event moves the status the reminder shows")
	require.Equal(t, "mailbox full", got.Email.StatusReason)
}

func TestReminderEmail_CustomerWithoutAddressCannotBeApproved(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	f.overdue(t, ctx, "a2", "cust_noemail", 100, 5)

	_, err := f.svc.UpdateReminder(ctx, "a2", setStatus(types.ReminderStatusApproved, ""))
	requireKind(t, err, ierr.ErrValidation)
	require.Equal(t, "This customer has no email address. Add one before approving.", errors.FlattenHints(err))

	got, err := f.svc.Get(ctx, "a2")
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, got.Status, "the approval is not recorded")
	require.Empty(t, got.CustomerEmail, "the response shows the missing address so a UI can ask for it")
	require.Empty(t, f.emails(t, ctx, "a2"), "no email row, not even a skipped one")
	require.Empty(t, f.reminders.All(ctx), "and no reminder row")
}

func TestReminderEmail_ApprovingFailsLoudlyWhereEmailsCannotBeSent(t *testing.T) {
	tests := []struct {
		name  string
		setup emailSetup
		kind  error
		hint  string
	}{
		{"deployment has no email provider", emailSetup{provider: false, enabled: true, replyTo: "billing@flex.test"}, ierr.ErrServiceUnavailable, "Emails are not available in this deployment"},
		{"environment emails switched off behind Collections", emailSetup{provider: true, enabled: false, replyTo: "billing@flex.test"}, ierr.ErrInvalidOperation, "Enable emails in this environment's settings to send them"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, ctx := emailFixture(t, tt.setup)

			_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
			requireKind(t, err, tt.kind)
			require.Equal(t, tt.hint, errors.FlattenHints(err))

			got, err := f.svc.Get(ctx, "a1")
			require.NoError(t, err)
			require.Equal(t, types.ReminderStatusPending, got.Status, "nothing is stored")
			require.Empty(t, f.emails(t, ctx, "a1"))
			require.Empty(t, f.reminders.All(ctx))
		})
	}
}

func TestReminderEmail_EnabledButTenantHasNoReplyToFailsLoudly(t *testing.T) {
	f, ctx := emailFixture(t, emailSetup{provider: true, enabled: true})

	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	requireKind(t, err, ierr.ErrInvalidOperation)

	got, err := f.svc.Get(ctx, "a1")
	require.NoError(t, err)
	require.Equal(t, types.ReminderStatusPending, got.Status, "the approval rolls back with the email")
	require.Empty(t, f.emails(t, ctx, "a1"))
}

func TestReminderEmail_BulkApproveFailsForACustomerWithoutAddress(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	f.overdue(t, ctx, "a2", "cust_noemail", 100, 5)
	f.overdue(t, ctx, "a3", "cust_1", 100, 5)

	_, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a2", "a1", "a3"}))
	requireKind(t, err, ierr.ErrValidation)
	require.Empty(t, f.queued(t, ctx), "no email is queued when the request fails")
}

func TestReminderEmail_BulkApproveQueuesOneEmailPerInvoice(t *testing.T) {
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
		require.Equal(t, types.EmailDeliveryStatusQueued, d.Email.Status, d.ID)
	}
	require.Empty(t, f.sent.all(), "bulk approving never calls the provider")
	_, err = f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a1", "a2", "a3"}))
	requireKind(t, err, ierr.ErrVersionConflict)

	require.Equal(t, 3, f.dispatch(t, ctx).Sent, "one run sends them all, once each")
	require.Len(t, f.sent.all(), 3)
	_, err = f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusPending, []string{"a1", "a2", "a3"}))
	requireKind(t, err, ierr.ErrVersionConflict)
}

func TestReminderEmail_BulkReopenCancelsQueuedEmails(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	f.overdue(t, ctx, "a2", "cust_1", 100, 5)
	_, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusApproved, []string{"a1", "a2"}))
	require.NoError(t, err)
	back, err := f.svc.BulkUpdateReminders(ctx, bulkStatus(types.ReminderStatusPending, []string{"a1", "a2"}))
	require.NoError(t, err)
	require.Len(t, back.Drafts, 2)
	for _, id := range []string{"a1", "a2"} {
		require.Equal(t, types.EmailDeliveryStatusSkipped, f.emails(t, ctx, id)[0].DeliveryStatus, id)
	}
	require.Equal(t, 0, f.dispatch(t, ctx).Sent)
}

func TestReminderEmail_ListCarriesTheEmailOfApprovedRemindersInOneQuery(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	for _, id := range []string{"a2", "a3", "a4"} {
		f.overdue(t, ctx, id, "cust_1", 100, 5)
	}
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	f.dispatch(t, ctx)
	_, err = f.svc.UpdateReminder(ctx, "a2", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	_, err = f.svc.UpdateReminder(ctx, "a3", setStatus(types.ReminderStatusHeld, "wait"))
	require.NoError(t, err)

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

func TestReminderEmail_PDFThatCannotBeProducedNeverHoldsTheEmail(t *testing.T) {
	ctx := reminderCtxFor("tenant_a", "env_a")
	f := newReminderFixture(t, ctx)
	f.customer(t, ctx, "cust_1", "Acme", "ap@acme.test")
	f.overdue(t, ctx, "a1", "cust_1", 100, 5)
	f.withEmail(t, ctx, emailSetup{provider: true, enabled: true, attachPDF: true, replyTo: "billing@flex.test"})

	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	res := f.dispatch(t, ctx)
	require.Equal(t, 1, res.Sent, "no PDF, and the dispatcher still sends on its first run")
	require.Zero(t, res.Deferred)
	require.Len(t, f.sent.all(), 1)
	require.Empty(t, f.sent.all()[0].Attachments)
	e := f.emails(t, ctx, "a1")[0]
	require.Equal(t, emailPDFUnavailable, e.Metadata[emailMetaPDF])
	require.Empty(t, f.queued(t, ctx), "nothing is left queued")
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
	f.dispatch(t, ctx)
	e := f.emails(t, ctx, "a1")[0]
	require.Equal(t, types.EmailDeliveryStatusSent, e.DeliveryStatus)
	require.Equal(t, emailPDFInvoiceLink, e.Metadata[emailMetaPDF], "a PDF that can be linked is still linked")
	require.Contains(t, f.sent.all()[0].Text, "https://files.test/a1.pdf")
}

func TestReminderEmail_WithoutThePDFSettingNothingIsRecordedAboutIt(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	f.dispatch(t, ctx)
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

// cancelsOnRead cancels the reminder email right after DeliverEmail first reads it as queued: a reopen that lands
// before the send but after the dispatcher looked.
type cancelsOnRead struct {
	emaillog.Repository
	cancel func()
	once   bool
}

func (c *cancelsOnRead) Get(ctx context.Context, id string) (*emaillog.EmailLog, error) {
	e, err := c.Repository.Get(ctx, id)
	if err == nil && !c.once {
		c.once = true
		c.cancel()
	}
	return e, err
}

func TestReminderEmail_ReopenBetweenTheReadAndTheSendIsNotEmailed(t *testing.T) {
	f, ctx := emailFixture(t, emailsOn)
	_, err := f.svc.UpdateReminder(ctx, "a1", setStatus(types.ReminderStatusApproved, ""))
	require.NoError(t, err)
	queued := f.queued(t, ctx)
	require.Len(t, queued, 1)

	params := f.params(f.reminders)
	svc := NewEmailNotificationService(params)
	params.EmailLogRepo = &cancelsOnRead{Repository: params.EmailLogRepo, cancel: func() {
		require.NoError(t, svc.CancelReminderEmail(ctx, lo.FromPtr(queued[0].IdempotencyKey)))
	}}
	require.NoError(t, NewEmailNotificationService(params).DeliverEmail(ctx, queued[0].ID, false))

	require.Empty(t, f.sent.all(), "the reopened reminder is not emailed")
	logs := f.emails(t, ctx, "a1")
	require.Len(t, logs, 1)
	require.Equal(t, types.EmailDeliveryStatusSkipped, logs[0].DeliveryStatus, "and the log says why it was not sent")
}
