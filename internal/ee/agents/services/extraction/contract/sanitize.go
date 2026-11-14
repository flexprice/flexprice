package contract

import (
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	"github.com/samber/lo"
)

type termVisitor func(path string, t *extraction.Term, allowed []string)

func (s *ContractExtractionService) eachTerm(r *extraction.Result, visit termVisitor) {
	visit("contract_type", &r.ContractType, contractTypes)

	c := &r.Customer
	visit("customer.legal_name", &c.LegalName, nil)
	visit("customer.billing_address", &c.BillingAddress, nil)
	visit("customer.primary_contact", &c.PrimaryContact, nil)
	visit("customer.billing_email", &c.BillingEmail, nil)

	ct := &r.Term
	visit("term.signed_date", &ct.SignedDate, nil)
	visit("term.effective_date", &ct.EffectiveDate, nil)
	visit("term.term_months", &ct.TermMonths, nil)
	visit("term.auto_renew", &ct.AutoRenew, []string{"true", "false"})
	visit("term.renewal_term_months", &ct.RenewalTermMonths, nil)
	visit("term.notice_days", &ct.NoticeDays, nil)
	visit("term.renewal_price_change", &ct.RenewalPriceChange, nil)
	visit("term.cancellation", &ct.Cancellation, nil)

	b := &r.Billing
	visit("billing.structure", &b.Structure, nil)
	visit("billing.currency", &b.Currency, nil)
	visit("billing.invoice_day", &b.InvoiceDay, nil)
	visit("billing.payment_terms", &b.PaymentTerms, nil)
	visit("billing.late_fee", &b.LateFee, nil)
	visit("billing.tax", &b.Tax, nil)

	for i := range r.Charges {
		ch, p := &r.Charges[i], "charges["+strconv.Itoa(i)+"]."
		visit(p+"name", &ch.Name, nil)
		visit(p+"amount", &ch.Amount, nil)
		visit(p+"amount_period", &ch.AmountPeriod, billingPeriods)
		visit(p+"billing_period", &ch.BillingPeriod, billingPeriods)
		visit(p+"billing_timing", &ch.BillingTiming, billingTimings)
		visit(p+"included_quantity", &ch.IncludedQuantity, nil)
		visit(p+"unit", &ch.Unit, nil)
		visit(p+"tier_mode", &ch.TierMode, tierModes)
		visit(p+"trigger", &ch.Trigger, nil)
		visit(p+"revenue_recognition", &ch.RevenueRecognition, nil)
	}
	for i := range r.Commitments {
		cm, p := &r.Commitments[i], "commitments["+strconv.Itoa(i)+"]."
		visit(p+"amount", &cm.Amount, nil)
		visit(p+"period", &cm.Period, billingPeriods)
		visit(p+"covers", &cm.Covers, nil)
		visit(p+"shortfall", &cm.Shortfall, nil)
	}
	for i := range r.Credits {
		cr, p := &r.Credits[i], "credits["+strconv.Itoa(i)+"]."
		visit(p+"amount", &cr.Amount, nil)
		visit(p+"kind", &cr.Kind, nil)
		visit(p+"expiry", &cr.Expiry, nil)
	}
	for i := range r.ServiceLevels {
		visit("service_levels["+strconv.Itoa(i)+"].description", &r.ServiceLevels[i].Description, nil)
	}
}

func (s *ContractExtractionService) sanitize(r *extraction.Result) {
	s.eachTerm(r, func(_ string, t *extraction.Term, allowed []string) { s.sanitizeTerm(t, allowed) })

	for i := range r.Documents {
		d := &r.Documents[i]
		d.Type = strings.ToLower(strings.TrimSpace(d.Type))
		if !lo.Contains(documentTypes, d.Type) {
			d.Type = DocumentTypeOther
		}
		d.PageStart, d.PageEnd = max(d.PageStart, 0), max(d.PageEnd, 0)
	}
	for i := range r.Charges {
		ch := &r.Charges[i]
		ch.Kind = strings.ToLower(strings.TrimSpace(ch.Kind))
		if !lo.Contains(chargeKinds, ch.Kind) {
			ch.Kind = ""
		}
		if ch.Tiers == nil {
			ch.Tiers = []extraction.Tier{}
		}
	}

	if r.Documents == nil {
		r.Documents = []extraction.Document{}
	}
	if r.Charges == nil {
		r.Charges = []extraction.Charge{}
	}
	if r.Commitments == nil {
		r.Commitments = []extraction.Commitment{}
	}
	if r.Credits == nil {
		r.Credits = []extraction.Credit{}
	}
	if r.ServiceLevels == nil {
		r.ServiceLevels = []extraction.ServiceLevel{}
	}
	if r.Doubts == nil {
		r.Doubts = []string{}
	}
}

func (s *ContractExtractionService) sanitizeTerm(t *extraction.Term, allowed []string) {
	t.Value = strings.TrimSpace(t.Value)
	t.Quote = strings.TrimSpace(t.Quote)
	t.Note = strings.TrimSpace(t.Note)
	t.Options = lo.Filter(lo.Map(t.Options, func(o string, _ int) string { return strings.TrimSpace(o) }),
		func(o string, _ int) bool { return o != "" })
	t.Derived = false

	if len(allowed) > 0 && t.Value != "" {
		if v, ok := s.matchAllowed(t.Value, allowed); ok {
			t.Value = v
		} else {
			t.Note = strings.TrimSpace(t.Note + " Read as \"" + t.Value + "\", which is not an allowed value.")
			t.Value = ""
			t.Confidence = 0
		}
	}

	t.Confidence = min(max(t.Confidence, 0), maxConfidence)
	t.Page = max(t.Page, 0)
	if t.Value == "" && len(t.Options) == 0 {
		t.Confidence, t.Page, t.Quote = 0, 0, ""
	}
}

func (s *ContractExtractionService) matchAllowed(v string, allowed []string) (string, bool) {
	for _, a := range allowed {
		if strings.EqualFold(v, a) {
			return a, true
		}
	}
	return "", false
}
