package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/email/render"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// Metadata keys on a collections team notice's email log.
const (
	emailMetaNoticeBody    = "notice_body"
	emailMetaNoticeHeading = "notice_heading"
)

// TeamNotice is an internal alert or task a playbook step emails to the merchant's team.
type TeamNotice struct {
	InvoiceID  string
	CustomerID string
	// StepRunID makes the email idempotent: a retried step returns the email it already queued.
	StepRunID  string
	Recipients []string
	Subject    string
	Heading    string
	Body       string
}

func teamNoticeKey(stepRunID string) string {
	return fmt.Sprintf("collections_notice:%s", stepRunID)
}

// QueueTeamNotice records the notice for DispatchEmails; a retried step returns the email it already queued.
func (s *emailNotificationService) QueueTeamNotice(ctx context.Context, n TeamNotice) (*emaillog.EmailLog, error) {
	key := teamNoticeKey(n.StepRunID)
	existing, err := s.EmailLogRepo.GetByIdempotencyKey(ctx, key)
	if err == nil {
		return existing, nil
	}
	if !ierr.IsNotFound(err) {
		return nil, err
	}
	inv, err := s.InvoiceRepo.Get(ctx, n.InvoiceID)
	if err != nil {
		return nil, err
	}
	merchant, err := invoiceMerchant(ctx, s.ServiceParams, inv)
	if err != nil {
		return nil, err
	}
	return s.queue(ctx, outgoingEmail{
		entityType:  types.SystemEntityTypeInvoice,
		entityID:    n.InvoiceID,
		customerID:  n.CustomerID,
		templateKey: types.EmailTemplateCollectionsTeamNotice,
		// Manual, not auto: email_logs allows one automatic email per invoice and template, and a playbook may send
		// several notices about one invoice. The per-step idempotency key keeps a retried step to one email.
		trigger:        types.EmailTriggerManual,
		subject:        n.Subject,
		to:             n.Recipients,
		senderName:     merchant.Name,
		replyTo:        merchant.BillingDetails.ReplyToEmail(),
		idempotencyKey: &key,
		metadata:       types.Metadata{emailMetaNoticeBody: n.Body, emailMetaNoticeHeading: n.Heading},
	})
}

// composeTeamNotice lays the notice text, frozen when the step ran, into the shared layout.
func (s *emailNotificationService) composeTeamNotice(ctx context.Context, e *emaillog.EmailLog) (*composedEmail, error) {
	body := e.Metadata[emailMetaNoticeBody]
	if body == "" {
		return nil, ierr.NewError("team notice has no text").
			WithHint("The notice text is missing from this email").
			Mark(ierr.ErrValidation)
	}
	invoiceSvc := NewInvoiceService(s.ServiceParams)
	inv, err := invoiceSvc.GetInvoice(ctx, e.EntityID)
	if err != nil {
		return nil, err
	}
	if err := invoiceSvc.WithHostedInvoiceURLs(ctx, inv); err != nil {
		return nil, err
	}
	merchant, err := invoiceMerchant(ctx, s.ServiceParams, &inv.Invoice)
	if err != nil {
		return nil, err
	}
	content, err := render.CollectionsTeamNotice(e.Subject, render.TeamNotice{
		Merchant:     merchant.Name,
		ContactEmail: merchant.BillingDetails.ReplyToEmail(),
		Heading:      e.Metadata[emailMetaNoticeHeading],
		Body:         body,
		ViewURL:      inv.HostedInvoiceURL,
	})
	if err != nil {
		return nil, err
	}
	return &composedEmail{content: content, metadata: map[string]string{}}, nil
}
