package service

import (
	"context"
	"fmt"
	"net/mail"
	"strings"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/email"
	"github.com/flexprice/flexprice/internal/email/emailtypes"
	"github.com/flexprice/flexprice/internal/email/render"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/temporal/models"
	temporalservice "github.com/flexprice/flexprice/internal/temporal/service"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// EmailNotificationService sends emails to customers and records each one in email_log.
type EmailNotificationService interface {
	// SendInvoiceEmail queues the email for a finalized invoice and starts its delivery.
	SendInvoiceEmail(ctx context.Context, invoiceID string, req *dto.SendInvoiceEmailRequest) (*dto.EmailLogResponse, error)

	// ListInvoiceEmails returns the emails sent for an invoice, newest first.
	ListInvoiceEmails(ctx context.Context, invoiceID string) (*dto.ListEmailLogsResponse, error)

	// DeliverEmail renders a queued email and hands it to the provider. With allowMissingAttachments
	// it sends without attachments it cannot produce; otherwise those are retryable errors.
	// Errors marked ierr.ErrValidation are permanent: the email is already marked failed.
	DeliverEmail(ctx context.Context, emailLogID string, allowMissingAttachments bool) error

	// MarkEmailFailed records that delivery gave up on a queued email.
	MarkEmailFailed(ctx context.Context, emailLogID string, reason string) error
}

type emailNotificationService struct {
	ServiceParams
}

func NewEmailNotificationService(params ServiceParams) EmailNotificationService {
	return &emailNotificationService{ServiceParams: params}
}

// outgoingEmail is what every email type supplies to queue an email.
type outgoingEmail struct {
	entityType  types.SystemEntityType
	entityID    string
	customerID  string
	templateKey types.EmailTemplateKey
	trigger     types.EmailTrigger
	subject     string
	to          []string
	cc          []string
}

// composedEmail is an email type's rendered content for one email log.
type composedEmail struct {
	content     render.Content
	attachments []email.Attachment
	metadata    map[string]string
}

func (s *emailNotificationService) SendInvoiceEmail(ctx context.Context, invoiceID string, req *dto.SendInvoiceEmailRequest) (*dto.EmailLogResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return nil, err
	}
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		return nil, ierr.NewError("invoice is not finalized").
			WithHint("Only finalized invoices can be emailed").
			WithReportableDetails(map[string]any{"invoice_id": invoiceID, "invoice_status": inv.InvoiceStatus}).
			Mark(ierr.ErrInvalidOperation)
	}

	to := req.To
	if len(to) == 0 {
		cust, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
		if err != nil {
			return nil, err
		}
		if cust.Email != "" {
			to = []string{cust.Email}
		}
	}

	merchant, err := invoiceMerchant(ctx, s.ServiceParams, inv)
	if err != nil {
		return nil, err
	}

	e, err := s.queue(ctx, outgoingEmail{
		entityType:  types.SystemEntityTypeInvoice,
		entityID:    inv.ID,
		customerID:  inv.CustomerID,
		templateKey: types.EmailTemplateInvoiceFinalized,
		trigger:     types.EmailTriggerManual,
		subject:     invoiceEmailSubject(inv, merchant.Name),
		to:          to,
		cc:          req.CC,
	})
	if err != nil {
		return nil, err
	}
	return dto.NewEmailLogResponse(e), nil
}

func (s *emailNotificationService) ListInvoiceEmails(ctx context.Context, invoiceID string) (*dto.ListEmailLogsResponse, error) {
	if _, err := s.InvoiceRepo.Get(ctx, invoiceID); err != nil {
		return nil, err
	}

	filter := types.NewEmailLogFilter()
	filter.EntityType = types.SystemEntityTypeInvoice
	filter.EntityIDs = []string{invoiceID}

	items, err := s.EmailLogRepo.List(ctx, filter)
	if err != nil {
		return nil, err
	}
	total, err := s.EmailLogRepo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}

	resp := types.NewListResponse(lo.Map(items, func(e *emaillog.EmailLog, _ int) *dto.EmailLogResponse {
		return dto.NewEmailLogResponse(e)
	}), total, filter.GetLimit(), filter.GetOffset())
	return &resp, nil
}

// queue checks the sender and recipients, records the email as queued and starts its delivery.
func (s *emailNotificationService) queue(ctx context.Context, out outgoingEmail) (*emaillog.EmailLog, error) {
	if !s.Config.Email.Enabled {
		return nil, ierr.NewError("customer emails are disabled").
			WithHint("Emails to customers are not enabled in this deployment").
			Mark(ierr.ErrInvalidOperation)
	}

	to := normalizeRecipients(out.to)
	if len(to) == 0 {
		return nil, ierr.NewError("no email recipient").
			WithHint("Add an email address to the customer, or enter one to send to").
			Mark(ierr.ErrValidation)
	}

	t, err := s.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil {
		return nil, err
	}
	replyTo := t.BillingDetails.ReplyToEmail()
	if replyTo == "" {
		return nil, ierr.NewError("tenant has no reply-to email").
			WithHint("Add a billing or help email in your business settings so customers can reply").
			Mark(ierr.ErrInvalidOperation)
	}

	e := &emaillog.EmailLog{
		ID:             types.GenerateUUIDWithPrefix(types.UUID_PREFIX_EMAIL_LOG),
		EnvironmentID:  types.GetEnvironmentID(ctx),
		EntityType:     out.entityType,
		EntityID:       out.entityID,
		CustomerID:     out.customerID,
		TemplateKey:    out.templateKey,
		Trigger:        out.trigger,
		FromAddress:    senderAddress(s.Config.Email.SenderLocalPart, s.Config.Email.SenderDomain, t.ID, t.Name),
		ReplyTo:        replyTo,
		ToAddresses:    to,
		CCAddresses:    normalizeRecipients(out.cc),
		Subject:        out.subject,
		Provider:       s.EmailProvider.Name(),
		DeliveryStatus: types.EmailDeliveryStatusQueued,
		Metadata:       types.Metadata{},
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	if err := s.EmailLogRepo.Create(ctx, e); err != nil {
		return nil, err
	}

	_, err = temporalservice.GetGlobalTemporalService().ExecuteWorkflow(ctx, types.TemporalSendEmailWorkflow, models.SendEmailWorkflowInput{
		EmailLogID:    e.ID,
		TenantID:      e.TenantID,
		EnvironmentID: e.EnvironmentID,
		UserID:        types.GetUserID(ctx),
	})
	if err != nil {
		s.Logger.Error(ctx, "failed to start email delivery", "error", err, "email_log_id", e.ID)
		if markErr := s.MarkEmailFailed(ctx, e.ID, "Delivery could not be started"); markErr != nil {
			s.Logger.Error(ctx, "failed to mark email failed", "error", markErr, "email_log_id", e.ID)
		}
		return nil, err
	}

	s.Logger.Info(ctx, "email queued", "email_log_id", e.ID, "entity_id", e.EntityID, "template_key", e.TemplateKey)
	return e, nil
}

func (s *emailNotificationService) DeliverEmail(ctx context.Context, emailLogID string, allowMissingAttachments bool) error {
	e, err := s.EmailLogRepo.Get(ctx, emailLogID)
	if err != nil {
		return err
	}
	if e.DeliveryStatus != types.EmailDeliveryStatusQueued {
		return nil
	}

	composed, err := s.compose(ctx, e, allowMissingAttachments)
	if err != nil {
		return s.failIfPermanent(ctx, e, err)
	}

	res, err := s.EmailProvider.Send(ctx, email.OutboundEmail{
		IdempotencyKey: e.ID,
		From:           e.FromAddress,
		ReplyTo:        e.ReplyTo,
		To:             e.ToAddresses,
		CC:             e.CCAddresses,
		Subject:        e.Subject,
		HTML:           composed.content.HTML,
		Text:           composed.content.Text,
		Attachments:    composed.attachments,
		Headers:        map[string]string{"Message-ID": fmt.Sprintf("<%s@%s>", e.ID, s.Config.Email.SenderDomain)},
		Tags: map[string]string{
			emailtypes.TagTenantID:      e.TenantID,
			emailtypes.TagEnvironmentID: e.EnvironmentID,
			emailtypes.TagEmailLogID:    e.ID,
			emailtypes.TagRegion:        s.Config.Email.RegionTag,
		},
	})
	if err != nil {
		return s.failIfPermanent(ctx, e, err)
	}

	b := emaillog.NewEmailLogBuilder(e).
		WithProviderMessageID(res.ProviderMessageID).
		WithDeliveryStatus(types.EmailDeliveryStatusSent).
		WithSentAt(clock.Now())
	for k, v := range composed.metadata {
		b = b.WithMetadata(k, v)
	}
	if err := s.EmailLogRepo.Update(ctx, b.Build()); err != nil {
		return err
	}

	s.Logger.Info(ctx, "email sent", "email_log_id", e.ID, "provider", e.Provider)
	return nil
}

func (s *emailNotificationService) MarkEmailFailed(ctx context.Context, emailLogID string, reason string) error {
	e, err := s.EmailLogRepo.Get(ctx, emailLogID)
	if err != nil {
		return err
	}
	if e.DeliveryStatus != types.EmailDeliveryStatusQueued {
		return nil
	}
	return s.EmailLogRepo.Update(ctx, emaillog.NewEmailLogBuilder(e).
		WithDeliveryStatus(types.EmailDeliveryStatusFailed).
		WithStatusReason(reason).
		Build())
}

// compose renders an email log with its email type's composer.
func (s *emailNotificationService) compose(ctx context.Context, e *emaillog.EmailLog, allowMissingAttachments bool) (*composedEmail, error) {
	switch e.TemplateKey {
	case types.EmailTemplateInvoiceFinalized:
		return s.composeInvoiceEmail(ctx, e, allowMissingAttachments)
	default:
		return nil, ierr.NewErrorf("no composer for email template %s", e.TemplateKey).
			WithHint("This email type cannot be sent").
			Mark(ierr.ErrValidation)
	}
}

// failIfPermanent marks the email failed when retrying cannot help, and returns err either way.
func (s *emailNotificationService) failIfPermanent(ctx context.Context, e *emaillog.EmailLog, err error) error {
	if !ierr.IsValidation(err) && !ierr.IsInvalidOperation(err) && !ierr.IsNotFound(err) {
		return err
	}
	reason := cerrors.FlattenHints(err)
	if reason == "" {
		reason = "The email could not be delivered"
	}
	s.Logger.Error(ctx, "email failed permanently", "error", err, "email_log_id", e.ID)
	if markErr := s.MarkEmailFailed(ctx, e.ID, reason); markErr != nil {
		return markErr
	}
	return ierr.WithError(err).WithHint(reason).Mark(ierr.ErrValidation)
}

// senderAddress is the tenant's From address on the shared sending domain.
func senderAddress(localPart, domain, tenantID, tenantName string) string {
	return (&mail.Address{
		Name:    tenantName,
		Address: fmt.Sprintf("%s+%s@%s", localPart, strings.ToLower(tenantID), domain),
	}).String()
}

// normalizeRecipients trims and lowercases addresses and drops blanks and repeats, keeping order.
func normalizeRecipients(addrs []string) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		a = strings.ToLower(strings.TrimSpace(a))
		if a != "" && !lo.Contains(out, a) {
			out = append(out, a)
		}
	}
	return out
}
