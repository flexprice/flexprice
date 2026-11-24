package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/collections"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/email/render"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// Metadata keys on a payment reminder email's log.
const (
	emailMetaReminderBody = "reminder_body"
	emailMetaReminderID   = "reminder_id"
	emailMetaOriginalTo   = "original_to"
)

// ReminderEmail is an approved payment reminder, frozen for sending.
type ReminderEmail struct {
	InvoiceID  string
	CustomerID string
	ReminderID string
	Recipient  string
	Subject    string
	Body       string
	ApprovedAt time.Time
}

// reminderEmailKey is stable for one approval: a repeated approval returns its email, a later one makes a new email.
func reminderEmailKey(invoiceID, reminderID string, approvedAt time.Time) string {
	return fmt.Sprintf("reminder:%s:%s:%d", invoiceID, reminderID, approvedAt.UnixMicro())
}

func (s *emailNotificationService) QueueReminderEmail(ctx context.Context, r ReminderEmail) (*emaillog.EmailLog, error) {
	key := reminderEmailKey(r.InvoiceID, r.ReminderID, r.ApprovedAt)
	existing, err := s.EmailLogRepo.GetByIdempotencyKey(ctx, key)
	if err == nil {
		return existing, nil
	}
	if !ierr.IsNotFound(err) {
		return nil, err
	}

	inv, err := s.InvoiceRepo.Get(ctx, r.InvoiceID)
	if err != nil {
		return nil, err
	}
	merchant, err := invoiceMerchant(ctx, s.ServiceParams, inv)
	if err != nil {
		return nil, err
	}
	return s.queue(ctx, outgoingEmail{
		entityType:     types.SystemEntityTypeInvoice,
		entityID:       r.InvoiceID,
		customerID:     r.CustomerID,
		templateKey:    types.EmailTemplateInvoicePaymentReminder,
		trigger:        types.EmailTriggerManual,
		subject:        r.Subject,
		to:             lo.Compact([]string{r.Recipient}),
		senderName:     merchant.Name,
		replyTo:        merchant.BillingDetails.ReplyToEmail(),
		idempotencyKey: &key,
		metadata:       types.Metadata{emailMetaReminderBody: r.Body, emailMetaReminderID: r.ReminderID},
		bestEffort:     true,
	})
}

func (s *emailNotificationService) CancelReminderEmail(ctx context.Context, key string) error {
	e, err := s.EmailLogRepo.GetByIdempotencyKey(ctx, key)
	if ierr.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	emailed := false
	err = s.transition(ctx, e.ID, func(cur *emaillog.EmailLog) *emaillog.EmailLog {
		switch {
		case cur.DeliveryStatus == types.EmailDeliveryStatusQueued:
			return emaillog.NewEmailLogBuilder(cur).
				WithDeliveryStatus(types.EmailDeliveryStatusSkipped).
				WithStatusReason("The reminder was reopened before this email was sent").
				Build()
		case !cur.DeliveryStatus.IsFinal():
			emailed = true
		}
		return nil
	})
	if err != nil {
		return err
	}
	if emailed {
		return ierr.NewError("reminder already emailed").
			WithHint("This reminder was already emailed to the customer, so it cannot be reopened.").
			Mark(ierr.ErrVersionConflict)
	}
	return nil
}

// reminderSendsWithoutPDF is the policy for reminders: an invoice PDF that cannot be produced never holds the email back,
// it goes out at once without it (the invoice email still waits for its PDF).
const reminderSendsWithoutPDF = true

// composeReminderEmail lays the text frozen at approval into the shared layout; it never rewrites the text.
func (s *emailNotificationService) composeReminderEmail(ctx context.Context, e *emaillog.EmailLog) (*composedEmail, error) {
	body := e.Metadata[emailMetaReminderBody]
	if body == "" {
		return nil, ierr.NewError("reminder email has no text").
			WithHint("The approved reminder text is missing from this email").
			Mark(ierr.ErrValidation)
	}

	invoiceSvc := NewInvoiceService(s.ServiceParams)
	inv, err := invoiceSvc.GetInvoice(ctx, e.EntityID)
	if err != nil {
		return nil, err
	}
	if !invoiceStillOwed(inv.InvoiceStatus, inv.PaymentStatus, inv.AmountRemaining.IsPositive()) {
		return nil, ierr.NewError("invoice is no longer owed").
			WithHint("The invoice was paid or voided before the reminder was sent").
			Mark(ierr.ErrInvalidOperation)
	}
	if err := invoiceSvc.WithHostedInvoiceURLs(ctx, inv); err != nil {
		return nil, err
	}
	merchant, err := invoiceMerchant(ctx, s.ServiceParams, &inv.Invoice)
	if err != nil {
		return nil, err
	}

	view := render.Invoice{Number: invoiceNumber(&inv.Invoice), ViewURL: inv.HostedInvoiceURL}
	composed := &composedEmail{metadata: map[string]string{}}
	if err := s.attachInvoiceFile(ctx, e, invoiceSvc, inv, &view, composed, reminderSendsWithoutPDF); err != nil {
		return nil, err
	}

	composed.content, err = render.PaymentReminder(e.Subject, render.Reminder{
		Merchant:     merchant.Name,
		ContactEmail: merchant.BillingDetails.ReplyToEmail(),
		Body:         body,
		ViewURL:      view.ViewURL,
		PDFAttached:  view.PDFAttached,
		PDFURL:       view.PDFURL,
	})
	return composed, err
}

// invoiceStillOwed reports an invoice a reminder can still be about: finalized and not paid.
func invoiceStillOwed(status types.InvoiceStatus, payment types.PaymentStatus, hasBalance bool) bool {
	return status == types.InvoiceStatusFinalized && payment != types.PaymentStatusSucceeded && hasBalance
}

const (
	// reminderSendWait bounds how long an approval waits to send its emails; unsent ones stay queued for the dispatcher.
	reminderSendWait = 5 * time.Second
	// reminderSendWaitWithAttachment is the bound when the emails carry a PDF: the request body is a few hundred KB
	// and the provider checks the attachment before it answers.
	reminderSendWaitWithAttachment = 20 * time.Second
	// reminderSendWorkers bounds how many emails of one bulk approval are handed to the provider at once.
	reminderSendWorkers = 8
)

// sendNow hands just-queued reminder emails to the provider, a few at a time, within reminderSendWait. It never fails
// the approval: an email it cannot send stays queued for the dispatcher.
func (s *reminderService) sendNow(ctx context.Context, queued []*emaillog.EmailLog) {
	pending := lo.Filter(queued, func(e *emaillog.EmailLog, _ int) bool {
		return e != nil && e.DeliveryStatus == types.EmailDeliveryStatusQueued
	})
	if len(pending) == 0 {
		return
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.sendWait(ctx))
	defer cancel()

	emails := NewEmailNotificationService(s.ServiceParams)
	slots := make(chan struct{}, reminderSendWorkers)
	var wg sync.WaitGroup
	for _, e := range pending {
		slots <- struct{}{}
		wg.Add(1)
		go func(e *emaillog.EmailLog) {
			defer func() { <-slots; wg.Done() }()
			if err := emails.DeliverEmail(sendCtx, e.ID, false); err != nil {
				s.Logger.Info(sendCtx, "reminder email left queued for the dispatcher", "error", err.Error(), "email_log_id", e.ID, "invoice_id", e.EntityID)
			}
		}(e)
	}
	wg.Wait()
}

// sendWait is how long an approval waits for its emails to go out: longer when they carry an attachment.
func (s *reminderService) sendWait(ctx context.Context) time.Duration {
	cfg, err := GetSetting[types.EmailNotificationConfig](NewSettingsService(s.ServiceParams).(*settingsService), ctx, types.SettingKeyEmailNotificationConfig)
	if err == nil && cfg.AttachInvoicePDF {
		return reminderSendWaitWithAttachment
	}
	return reminderSendWait
}

// reminderEmails is the latest email of each approved reminder in rows, in one query; fresh reads from the primary.
func (s *reminderService) reminderEmails(ctx context.Context, rows []collections.Row, fresh bool) (map[string]*dto.ReminderEmailStatus, error) {
	keys := map[string]string{}
	for _, r := range rows {
		if rem := r.Reminder; rem != nil && rem.ReminderStatus == types.ReminderStatusApproved && rem.ApprovedAt != nil {
			keys[r.InvoiceID] = reminderEmailKey(r.InvoiceID, rem.ID, *rem.ApprovedAt)
		}
	}
	emails := map[string]*dto.ReminderEmailStatus{}
	if len(keys) == 0 {
		return emails, nil
	}
	if fresh {
		ctx = types.WithForceWriter(ctx)
	}
	filter := types.NewEmailLogFilter()
	filter.QueryFilter = types.NewNoLimitQueryFilter()
	filter.EntityType = types.SystemEntityTypeInvoice
	filter.EntityIDs = lo.Keys(keys)
	logs, err := s.EmailLogRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, e := range logs {
		if e.TemplateKey == types.EmailTemplateInvoicePaymentReminder && lo.FromPtr(e.IdempotencyKey) == keys[e.EntityID] {
			emails[e.EntityID] = &dto.ReminderEmailStatus{
				Status: e.DeliveryStatus, To: lo.FirstOrEmpty(e.ToAddresses), SentAt: e.SentAt, StatusReason: lo.FromPtr(e.StatusReason),
			}
		}
	}
	return emails, nil
}
