package types

import "github.com/samber/lo"

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

// emailDeliveryProgress orders the statuses an email moves through on its way to the inbox.
var emailDeliveryProgress = []EmailDeliveryStatus{
	EmailDeliveryStatusQueued,
	EmailDeliveryStatusSent,
	EmailDeliveryStatusDeliveryDelayed,
	EmailDeliveryStatusDelivered,
	EmailDeliveryStatusComplained,
}

// IsFinal reports a status that never changes again.
func (s EmailDeliveryStatus) IsFinal() bool {
	return s == EmailDeliveryStatusBounced || s == EmailDeliveryStatusFailed ||
		s == EmailDeliveryStatusSuppressed || s == EmailDeliveryStatusSkipped
}

// CanMoveTo reports whether next may replace s: statuses only move forward, any non-final status
// may end as bounced, failed or suppressed, and final statuses never change.
func (s EmailDeliveryStatus) CanMoveTo(next EmailDeliveryStatus) bool {
	if s.IsFinal() {
		return false
	}
	if next == EmailDeliveryStatusSkipped {
		return s == EmailDeliveryStatusQueued
	}
	if next.IsFinal() {
		return true
	}
	return lo.IndexOf(emailDeliveryProgress, next) > lo.IndexOf(emailDeliveryProgress, s)
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
	EmailProviderLocal  EmailProvider = "local"
)

func (p EmailProvider) Validate() error {
	return validateEnum("provider", p, []EmailProvider{EmailProviderResend, EmailProviderLocal})
}

// EmailLogFilter selects email logs, newest first by default.
type EmailLogFilter struct {
	*QueryFilter
	*TimeRangeFilter

	EntityType       SystemEntityType      `json:"entity_type,omitempty" form:"entity_type" validate:"omitempty"`
	EntityIDs        []string              `json:"entity_ids,omitempty" form:"entity_ids" validate:"omitempty"`
	CustomerIDs      []string              `json:"customer_ids,omitempty" form:"customer_ids" validate:"omitempty"`
	DeliveryStatuses []EmailDeliveryStatus `json:"delivery_statuses,omitempty" form:"delivery_statuses" validate:"omitempty"`
	Triggers         []EmailTrigger        `json:"triggers,omitempty" form:"triggers" validate:"omitempty"`
}

func NewEmailLogFilter() *EmailLogFilter {
	return &EmailLogFilter{QueryFilter: NewDefaultQueryFilter()}
}

func (f *EmailLogFilter) Validate() error {
	if f.QueryFilter != nil {
		if err := f.QueryFilter.Validate(); err != nil {
			return err
		}
	}
	if f.TimeRangeFilter != nil {
		if err := f.TimeRangeFilter.Validate(); err != nil {
			return err
		}
	}
	for _, s := range f.DeliveryStatuses {
		if err := s.Validate(); err != nil {
			return err
		}
	}
	for _, t := range f.Triggers {
		if err := t.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// GetLimit implements BaseFilter interface
func (f *EmailLogFilter) GetLimit() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetLimit()
	}
	return f.QueryFilter.GetLimit()
}

// GetOffset implements BaseFilter interface
func (f *EmailLogFilter) GetOffset() int {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOffset()
	}
	return f.QueryFilter.GetOffset()
}

// GetSort implements BaseFilter interface
func (f *EmailLogFilter) GetSort() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetSort()
	}
	return f.QueryFilter.GetSort()
}

// GetOrder implements BaseFilter interface
func (f *EmailLogFilter) GetOrder() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetOrder()
	}
	return f.QueryFilter.GetOrder()
}

// GetStatus implements BaseFilter interface
func (f *EmailLogFilter) GetStatus() string {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetStatus()
	}
	return f.QueryFilter.GetStatus()
}

// GetExpand implements BaseFilter interface
func (f *EmailLogFilter) GetExpand() Expand {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().GetExpand()
	}
	return f.QueryFilter.GetExpand()
}

// IsUnlimited implements BaseFilter interface
func (f *EmailLogFilter) IsUnlimited() bool {
	if f.QueryFilter == nil {
		return NewDefaultQueryFilter().IsUnlimited()
	}
	return f.QueryFilter.IsUnlimited()
}
