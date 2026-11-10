package types

// EmailDeliveryStatus is where one sent email stands, from queued to its final outcome.
type EmailDeliveryStatus string

const (
	EmailDeliveryStatusQueued          EmailDeliveryStatus = "queued"
	EmailDeliveryStatusSent            EmailDeliveryStatus = "sent"
	EmailDeliveryStatusDeliveryDelayed EmailDeliveryStatus = "delivery_delayed"
	EmailDeliveryStatusDelivered       EmailDeliveryStatus = "delivered"
	EmailDeliveryStatusBounced         EmailDeliveryStatus = "bounced"
	EmailDeliveryStatusComplained      EmailDeliveryStatus = "complained"
	EmailDeliveryStatusFailed          EmailDeliveryStatus = "failed"
	EmailDeliveryStatusSuppressed      EmailDeliveryStatus = "suppressed"
	EmailDeliveryStatusSkipped         EmailDeliveryStatus = "skipped"
)

func (s EmailDeliveryStatus) Validate() error {
	return validateEnum("delivery_status", s, []EmailDeliveryStatus{
		EmailDeliveryStatusQueued,
		EmailDeliveryStatusSent,
		EmailDeliveryStatusDeliveryDelayed,
		EmailDeliveryStatusDelivered,
		EmailDeliveryStatusBounced,
		EmailDeliveryStatusComplained,
		EmailDeliveryStatusFailed,
		EmailDeliveryStatusSuppressed,
		EmailDeliveryStatusSkipped,
	})
}

// EmailTrigger says whether an email was sent automatically or by a user.
type EmailTrigger string

const (
	EmailTriggerAuto   EmailTrigger = "auto"
	EmailTriggerManual EmailTrigger = "manual"
)

func (t EmailTrigger) Validate() error {
	return validateEnum("trigger", t, []EmailTrigger{EmailTriggerAuto, EmailTriggerManual})
}

// EmailTemplateKey names the template an email was rendered from.
type EmailTemplateKey string

const (
	EmailTemplateInvoiceFinalized EmailTemplateKey = "invoice_finalized"
)

func (k EmailTemplateKey) Validate() error {
	return validateEnum("template_key", k, []EmailTemplateKey{EmailTemplateInvoiceFinalized})
}

// EmailProvider names the service that delivers an email.
type EmailProvider string

const (
	EmailProviderResend EmailProvider = "resend"
	EmailProviderLog    EmailProvider = "log"
)

func (p EmailProvider) Validate() error {
	return validateEnum("provider", p, []EmailProvider{EmailProviderResend, EmailProviderLog})
}
