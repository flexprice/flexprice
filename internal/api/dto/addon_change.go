package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/addonassociation"
	"github.com/flexprice/flexprice/internal/domain/subscription"
)

type AddonChangeResult struct {
	Association      *addonassociation.AddonAssociation
	CreatedLineItems []*subscription.SubscriptionLineItem
	EndedLineItems   []*subscription.SubscriptionLineItem
	ChangedInvoices  []ChangedInvoice

	// EffectiveDate is when the change takes effect.
	EffectiveDate   time.Time
	CheckoutSession *CheckoutSessionResponse
	Invoice         *InvoiceResponse
}

func (r *AddonChangeResult) GetAssociation() *addonassociation.AddonAssociation {
	if r == nil {
		return nil
	}
	return r.Association
}

func (r *AddonChangeResult) GetCreatedLineItems() []*subscription.SubscriptionLineItem {
	if r == nil {
		return nil
	}
	return r.CreatedLineItems
}

func (r *AddonChangeResult) GetEndedLineItems() []*subscription.SubscriptionLineItem {
	if r == nil {
		return nil
	}
	return r.EndedLineItems
}

func (r *AddonChangeResult) GetChangedInvoices() []ChangedInvoice {
	if r == nil {
		return nil
	}
	return r.ChangedInvoices
}

func (r *AddonChangeResult) GetEffectiveDate() time.Time {
	if r == nil {
		return time.Time{}
	}
	return r.EffectiveDate
}

func (r *AddonChangeResult) GetCheckoutSession() *CheckoutSessionResponse {
	if r == nil {
		return nil
	}
	return r.CheckoutSession
}

func (r *AddonChangeResult) GetInvoice() *InvoiceResponse {
	if r == nil {
		return nil
	}
	return r.Invoice
}

// PaymentPending reports whether the change is waiting on a checkout payment. Nothing has been
// applied while it is true, so callers must not announce the change as done.
func (r *AddonChangeResult) PaymentPending() bool {
	return r.GetCheckoutSession() != nil
}
