package contract

import (
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
)

const (
	stepCustomer = "customer"
	stepCharges  = "charges"
	stepBilling  = "billing"
)

func (c *contractIngestService) needsCheck(t extraction.Term, threshold int) bool {
	if t.Derived || t.Page == 0 {
		return false
	}
	return len(t.Options) > 0 || (t.IsSet() && t.Confidence < threshold)
}

func (c *contractIngestService) countToCheck(r *extraction.Result, threshold int) *extraction.ToCheck {
	tc := &extraction.ToCheck{}
	c.eachTerm(r, func(path string, t *extraction.Term) {
		if !c.needsCheck(*t, threshold) {
			return
		}
		switch c.stepFor(path) {
		case stepCustomer:
			tc.Customer++
		case stepCharges:
			tc.Charges++
		case stepBilling:
			tc.Billing++
		default:
			return
		}
		tc.Total++
	})
	return tc
}

// stepFor maps a term's JSON path to the review step it is confirmed on.
func (c *contractIngestService) stepFor(path string) string {
	switch {
	case strings.HasPrefix(path, "customer."):
		return stepCustomer
	case strings.HasPrefix(path, "term."), strings.HasPrefix(path, "billing."),
		strings.HasSuffix(path, ".billing_period"), strings.HasSuffix(path, ".billing_timing"):
		return stepBilling
	case strings.HasPrefix(path, "charges["), strings.HasPrefix(path, "commitments["),
		strings.HasPrefix(path, "credits["), strings.HasPrefix(path, "service_levels["):
		return stepCharges
	default:
		return ""
	}
}

// eachTerm calls fn for every extracted term with its JSON path, e.g. "customer.legal_name" or "charges[0].billing_period".
func (c *contractIngestService) eachTerm(r *extraction.Result, fn func(path string, t *extraction.Term)) {
	fn("contract_type", &r.ContractType)

	fn("customer.legal_name", &r.Customer.LegalName)
	fn("customer.billing_address", &r.Customer.BillingAddress)
	fn("customer.primary_contact", &r.Customer.PrimaryContact)
	fn("customer.billing_email", &r.Customer.BillingEmail)

	ct := &r.Term
	fn("term.signed_date", &ct.SignedDate)
	fn("term.effective_date", &ct.EffectiveDate)
	fn("term.term_months", &ct.TermMonths)
	fn("term.auto_renew", &ct.AutoRenew)
	fn("term.renewal_term_months", &ct.RenewalTermMonths)
	fn("term.notice_days", &ct.NoticeDays)
	fn("term.renewal_price_change", &ct.RenewalPriceChange)
	fn("term.cancellation", &ct.Cancellation)

	b := &r.Billing
	fn("billing.structure", &b.Structure)
	fn("billing.currency", &b.Currency)
	fn("billing.invoice_day", &b.InvoiceDay)
	fn("billing.payment_terms", &b.PaymentTerms)
	fn("billing.late_fee", &b.LateFee)
	fn("billing.tax", &b.Tax)

	for i := range r.Charges {
		ch, p := &r.Charges[i], "charges["+strconv.Itoa(i)+"]."
		fn(p+"name", &ch.Name)
		fn(p+"amount", &ch.Amount)
		fn(p+"amount_period", &ch.AmountPeriod)
		fn(p+"billing_period", &ch.BillingPeriod)
		fn(p+"billing_timing", &ch.BillingTiming)
		fn(p+"included_quantity", &ch.IncludedQuantity)
		fn(p+"unit", &ch.Unit)
		fn(p+"tier_mode", &ch.TierMode)
		fn(p+"trigger", &ch.Trigger)
		fn(p+"revenue_recognition", &ch.RevenueRecognition)
	}
	for i := range r.Commitments {
		cm, p := &r.Commitments[i], "commitments["+strconv.Itoa(i)+"]."
		fn(p+"amount", &cm.Amount)
		fn(p+"period", &cm.Period)
		fn(p+"covers", &cm.Covers)
		fn(p+"shortfall", &cm.Shortfall)
	}
	for i := range r.Credits {
		cr, p := &r.Credits[i], "credits["+strconv.Itoa(i)+"]."
		fn(p+"amount", &cr.Amount)
		fn(p+"kind", &cr.Kind)
		fn(p+"expiry", &cr.Expiry)
	}
	for i := range r.ServiceLevels {
		fn("service_levels["+strconv.Itoa(i)+"].description", &r.ServiceLevels[i].Description)
	}
}
