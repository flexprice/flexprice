package service

import (
	"context"
	"fmt"
	"net/http"
	"net/mail"
	"strings"

	cerrors "github.com/cockroachdb/errors"
	"github.com/flexprice/flexprice/internal/activity"
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/clock"
	"github.com/flexprice/flexprice/internal/domain/emaillog"
	"github.com/flexprice/flexprice/internal/email"
	"github.com/flexprice/flexprice/internal/email/emailtypes"
	"github.com/flexprice/flexprice/internal/email/render"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/interfaces"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
)

// EmailNotificationService sends emails to customers and records each one in email_log.
type EmailNotificationService interface {
	// SendInvoiceEmail queues the email for a finalized invoice; DispatchEmails sends it.
	SendInvoiceEmail(ctx context.Context, invoiceID string, req *dto.SendInvoiceEmailRequest) (*dto.EmailLogResponse, error)

	interfaces.InvoiceEmailSender

	// ListInvoiceEmails returns the emails sent for an invoice, newest first.
	ListInvoiceEmails(ctx context.Context, invoiceID string) (*dto.ListEmailLogsResponse, error)

	// ListEmails returns the emails matching filter, newest first unless it sorts otherwise.
	ListEmails(ctx context.Context, filter *types.EmailLogFilter) (*dto.ListEmailLogsResponse, error)

	// DeliverEmail renders a queued email and hands it to the provider. With allowMissingAttachments
	// it sends without attachments it cannot produce; otherwise those are retryable errors.
	// Errors marked ierr.ErrValidation are permanent: the email is already marked failed.
	DeliverEmail(ctx context.Context, emailLogID string, allowMissingAttachments bool) error

	// MarkEmailFailed records that delivery gave up on a queued email.
	MarkEmailFailed(ctx context.Context, emailLogID string, reason string) error

	// DispatchEmails sends queued emails from every environment at the deployment's send rate.
	DispatchEmails(ctx context.Context) (*EmailDispatchResult, error)

	// HandleDeliveryWebhook verifies a provider's delivery webhook and records each event on its email.
	// Events for another region, events without tenant tags and events for unknown emails are dropped.
	HandleDeliveryWebhook(ctx context.Context, provider types.EmailProvider, body []byte, header http.Header) error
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
	// senderName and replyTo fall back to the tenant's name and reply-to email when empty.
	senderName string
	replyTo    string
	// skipReason records the email as skipped instead of sending it.
	skipReason string
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
		senderName:  merchant.Name,
		replyTo:     merchant.BillingDetails.ReplyToEmail(),
	})
	if err != nil {
		return nil, err
	}
	return dto.NewEmailLogResponse(e), nil
}

func (s *emailNotificationService) SendAutoInvoiceEmail(ctx context.Context, invoiceID string) error {
	if !s.Config.Email.Enabled {
		return nil
	}
	cfg, err := s.settings(ctx)
	if err != nil {
		return err
	}
	if !cfg.AutoSends(types.SystemEntityTypeInvoice) {
		return nil
	}

	// The invoice was finalized moments ago; read it from the primary, not a lagging replica.
	ctx = types.WithForceWriter(ctx)
	inv, err := s.InvoiceRepo.Get(ctx, invoiceID)
	if err != nil {
		return err
	}
	if inv.InvoiceStatus != types.InvoiceStatusFinalized {
		s.Logger.Info(ctx, "automatic invoice email not sent: invoice is not finalized", "invoice_id", invoiceID, "invoice_status", inv.InvoiceStatus)
		return nil
	}
	cust, err := s.CustomerRepo.Get(ctx, inv.CustomerID)
	if err != nil {
		return err
	}
	merchant, err := invoiceMerchant(ctx, s.ServiceParams, inv)
	if err != nil {
		return err
	}

	out := outgoingEmail{
		entityType:  types.SystemEntityTypeInvoice,
		entityID:    inv.ID,
		customerID:  inv.CustomerID,
		templateKey: types.EmailTemplateInvoiceFinalized,
		trigger:     types.EmailTriggerAuto,
		subject:     invoiceEmailSubject(inv, merchant.Name),
		to:          lo.Compact([]string{cust.Email}),
		senderName:  merchant.Name,
		replyTo:     merchant.BillingDetails.ReplyToEmail(),
	}
	if inv.Total.IsZero() {
		out.skipReason = "Invoice total is zero"
	}

	_, err = s.queue(ctx, out)
	if ierr.IsAlreadyExists(err) {
		s.Logger.Info(ctx, "automatic invoice email already recorded", "invoice_id", invoiceID)
		return nil
	}
	return err
}

func (s *emailNotificationService) ListInvoiceEmails(ctx context.Context, invoiceID string) (*dto.ListEmailLogsResponse, error) {
	if _, err := s.InvoiceRepo.Get(ctx, invoiceID); err != nil {
		return nil, err
	}

	filter := types.NewEmailLogFilter()
	filter.EntityType = types.SystemEntityTypeInvoice
	filter.EntityIDs = []string{invoiceID}
	return s.ListEmails(ctx, filter)
}

func (s *emailNotificationService) ListEmails(ctx context.Context, filter *types.EmailLogFilter) (*dto.ListEmailLogsResponse, error) {
	if filter.QueryFilter == nil {
		filter.QueryFilter = types.NewDefaultQueryFilter()
	}
	if err := filter.Validate(); err != nil {
		return nil, err
	}

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

// queue checks the sender and recipients and records the email as queued for DispatchEmails.
// An automatic email that cannot be sent is recorded as skipped, with the reason.
func (s *emailNotificationService) queue(ctx context.Context, out outgoingEmail) (*emaillog.EmailLog, error) {
	if !s.Config.Email.IsProviderConfigured() {
		return nil, ierr.NewError("customer emails are disabled").
			WithHint("Emails to customers are not enabled in this deployment").
			Mark(ierr.ErrInvalidOperation)
	}
	cfg, err := GetSetting[types.EmailNotificationConfig](NewSettingsService(s.ServiceParams).(*settingsService), ctx, types.SettingKeyEmailNotificationConfig)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ierr.NewError("emails are disabled for this environment").
			WithHint("Enable emails in this environment's settings to send them").
			Mark(ierr.ErrInvalidOperation)
	}

	t, err := s.TenantRepo.GetByID(ctx, types.GetTenantID(ctx))
	if err != nil {
		return nil, err
	}
	to := normalizeRecipients(out.to)
	replyTo := lo.CoalesceOrEmpty(out.replyTo, t.BillingDetails.ReplyToEmail())
	auto := out.trigger == types.EmailTriggerAuto

	switch {
	case out.skipReason != "":
	case len(to) == 0 && auto:
		out.skipReason = "Customer has no email address"
	case len(to) == 0:
		return nil, ierr.NewError("no email recipient").
			WithHint("Add an email address to the customer, or enter one to send to").
			Mark(ierr.ErrValidation)
	case replyTo == "" && auto:
		out.skipReason = "No billing or help email for customers to reply to"
	case replyTo == "":
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
		FromAddress:    senderAddress(s.Config.Email.SenderLocalPart, s.Config.Email.SenderDomain, t.ID, lo.CoalesceOrEmpty(out.senderName, t.Name)),
		ReplyTo:        replyTo,
		ToAddresses:    to,
		CCAddresses:    normalizeRecipients(out.cc),
		Subject:        out.subject,
		Provider:       s.EmailProvider.Name(),
		DeliveryStatus: types.EmailDeliveryStatusQueued,
		Metadata:       types.Metadata{},
		BaseModel:      types.GetDefaultBaseModel(ctx),
	}
	if out.skipReason != "" {
		e.DeliveryStatus = types.EmailDeliveryStatusSkipped
		e.StatusReason = lo.ToPtr(out.skipReason)
		if err := s.EmailLogRepo.Create(activity.WithAction(ctx, e.ID, activity.EmailLogSkipped), e); err != nil {
			return nil, err
		}
		s.Logger.Info(ctx, "email skipped", "email_log_id", e.ID, "entity_id", e.EntityID, "reason", out.skipReason)
		return e, nil
	}
	if err := s.EmailLogRepo.Create(ctx, e); err != nil {
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

	sentAt := clock.Now()
	err = s.transition(ctx, e.ID, func(cur *emaillog.EmailLog) *emaillog.EmailLog {
		b := emaillog.NewEmailLogBuilder(cur)
		if cur.ProviderMessageID == nil {
			b = b.WithProviderMessageID(res.ProviderMessageID)
		}
		if cur.SentAt == nil {
			b = b.WithSentAt(sentAt)
		}
		if cur.DeliveryStatus.CanMoveTo(types.EmailDeliveryStatusSent) {
			b = b.WithDeliveryStatus(types.EmailDeliveryStatusSent)
		}
		for k, v := range composed.metadata {
			b = b.WithMetadata(k, v)
		}
		return b.Build()
	})
	if err != nil {
		return err
	}

	s.Logger.Info(ctx, "email sent", "email_log_id", e.ID, "provider", e.Provider)
	return nil
}

func (s *emailNotificationService) MarkEmailFailed(ctx context.Context, emailLogID string, reason string) error {
	return s.transition(ctx, emailLogID, func(cur *emaillog.EmailLog) *emaillog.EmailLog {
		if cur.DeliveryStatus != types.EmailDeliveryStatusQueued {
			return nil
		}
		return emaillog.NewEmailLogBuilder(cur).
			WithDeliveryStatus(types.EmailDeliveryStatusFailed).
			WithStatusReason(reason).
			Build()
	})
}

func (s *emailNotificationService) HandleDeliveryWebhook(ctx context.Context, provider types.EmailProvider, body []byte, header http.Header) error {
	if provider != s.EmailProvider.Name() {
		return ierr.NewErrorf("email provider %s is not configured", provider).
			WithHint("This deployment does not accept delivery events from this provider").
			Mark(ierr.ErrNotFound)
	}
	events, err := s.EmailProvider.ParseEvents(ctx, body, header)
	if err != nil {
		return err
	}
	for _, ev := range events {
		if err := s.recordDeliveryEvent(ctx, ev); err != nil {
			return err
		}
	}
	return nil
}

// recordDeliveryEvent applies one delivery event to its email, scoped to the tenant in its tags.
func (s *emailNotificationService) recordDeliveryEvent(ctx context.Context, ev email.DeliveryEvent) error {
	if ev.Region != s.Config.Email.RegionTag {
		s.Logger.Info(ctx, "email delivery event dropped", "reason", "other_region", "region", ev.Region, "provider_message_id", ev.ProviderMessageID)
		return nil
	}
	if ev.TenantID == "" || ev.EnvironmentID == "" || ev.EmailLogID == "" {
		s.Logger.Info(ctx, "email delivery event dropped", "reason", "missing_tags", "provider_message_id", ev.ProviderMessageID)
		return nil
	}
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = clock.Now()
	}

	ctx = types.SetTenantID(ctx, ev.TenantID)
	ctx = types.SetEnvironmentID(ctx, ev.EnvironmentID)
	err := s.transition(ctx, ev.EmailLogID, func(cur *emaillog.EmailLog) *emaillog.EmailLog {
		return applyDeliveryEvent(cur, ev)
	})
	if ierr.IsNotFound(err) {
		s.Logger.Info(ctx, "email delivery event dropped", "reason", "unknown_email", "email_log_id", ev.EmailLogID, "provider_message_id", ev.ProviderMessageID)
		return nil
	}
	if err != nil {
		return err
	}

	s.Logger.Info(ctx, "email delivery event recorded", "email_log_id", ev.EmailLogID, "status", ev.Status)
	return nil
}

// applyDeliveryEvent is the email after a delivery event, or nil when the event changes nothing:
// a stale or repeated status, or an event for a different provider message.
func applyDeliveryEvent(cur *emaillog.EmailLog, ev email.DeliveryEvent) *emaillog.EmailLog {
	if cur.ProviderMessageID != nil && *cur.ProviderMessageID != ev.ProviderMessageID {
		return nil
	}
	if !cur.DeliveryStatus.CanMoveTo(ev.Status) {
		return nil
	}

	b := emaillog.NewEmailLogBuilder(cur).WithDeliveryStatus(ev.Status)
	if cur.ProviderMessageID == nil {
		b = b.WithProviderMessageID(ev.ProviderMessageID)
	}
	if ev.Reason != "" {
		b = b.WithStatusReason(ev.Reason)
	}
	switch ev.Status {
	case types.EmailDeliveryStatusSent:
		if cur.SentAt == nil {
			b = b.WithSentAt(ev.OccurredAt)
		}
	case types.EmailDeliveryStatusDelivered:
		b = b.WithDeliveredAt(ev.OccurredAt)
	}
	return b.Build()
}

// transition changes an email log under a row lock, so the delivery workflow and delivery events
// cannot overwrite each other. change returns nil to leave the email as it is.
func (s *emailNotificationService) transition(ctx context.Context, id string, change func(*emaillog.EmailLog) *emaillog.EmailLog) error {
	return s.DB.WithTx(ctx, func(txCtx context.Context) error {
		cur, err := s.EmailLogRepo.GetForUpdate(txCtx, id)
		if err != nil {
			return err
		}
		next := change(cur)
		if next == nil {
			return nil
		}
		if action, ok := deliveryAction(next.DeliveryStatus); ok && next.DeliveryStatus != cur.DeliveryStatus {
			txCtx = activity.WithAction(txCtx, cur.ID, action)
		}
		return s.EmailLogRepo.Update(txCtx, next)
	})
}

// deliveryAction names the activity-log entry for an email reaching a delivery status.
func deliveryAction(status types.EmailDeliveryStatus) (activity.Action, bool) {
	switch status {
	case types.EmailDeliveryStatusSent:
		return activity.EmailLogSent, true
	case types.EmailDeliveryStatusDelivered:
		return activity.EmailLogDelivered, true
	case types.EmailDeliveryStatusBounced:
		return activity.EmailLogBounced, true
	case types.EmailDeliveryStatusComplained:
		return activity.EmailLogComplained, true
	case types.EmailDeliveryStatusFailed:
		return activity.EmailLogFailed, true
	case types.EmailDeliveryStatusSuppressed:
		return activity.EmailLogSuppressed, true
	default:
		return "", false
	}
}

func (s *emailNotificationService) settings(ctx context.Context) (types.EmailNotificationConfig, error) {
	return GetSetting[types.EmailNotificationConfig](NewSettingsService(s.ServiceParams).(*settingsService), ctx, types.SettingKeyEmailNotificationConfig)
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

// senderAddress is the tenant's From address on the shared sending domain, shown under senderName.
func senderAddress(localPart, domain, tenantID, senderName string) string {
	return (&mail.Address{
		Name:    senderName,
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
