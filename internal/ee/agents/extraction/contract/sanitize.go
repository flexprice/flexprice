package contract

import (
	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
	"strings"

	"github.com/samber/lo"
)

const (
	stepUpload   = "upload"
	stepCustomer = "customer"
	stepCharges  = "charges"
	stepBilling  = "billing"
)

type termVisitor func(step string, t *extraction.Term, allowed []string)

func eachTerm(r *extraction.Result, visit termVisitor) {
	visit(stepUpload, &r.ContractType, contractTypes)

	c := &r.Customer
	for _, t := range []*extraction.Term{&c.LegalName, &c.BillingAddress, &c.PrimaryContact, &c.BillingEmail} {
		visit(stepCustomer, t, nil)
	}

	ct := &r.Term
	for _, t := range []*extraction.Term{&ct.SignedDate, &ct.EffectiveDate, &ct.TermMonths, &ct.RenewalTermMonths,
		&ct.NoticeDays, &ct.RenewalPriceChange, &ct.Cancellation} {
		visit(stepBilling, t, nil)
	}
	visit(stepBilling, &ct.AutoRenew, []string{"true", "false"})

	b := &r.Billing
	for _, t := range []*extraction.Term{&b.Structure, &b.Currency, &b.InvoiceDay, &b.PaymentTerms, &b.LateFee, &b.Tax} {
		visit(stepBilling, t, nil)
	}

	for i := range r.Charges {
		ch := &r.Charges[i]
		for _, t := range []*extraction.Term{&ch.Name, &ch.Amount, &ch.IncludedQuantity, &ch.Unit, &ch.Trigger, &ch.RevenueRecognition} {
			visit(stepCharges, t, nil)
		}
		visit(stepCharges, &ch.AmountPeriod, billingPeriods)
		visit(stepBilling, &ch.BillingPeriod, billingPeriods)
		visit(stepBilling, &ch.BillingTiming, billingTimings)
		visit(stepCharges, &ch.TierMode, tierModes)
	}
	for i := range r.Commitments {
		cm := &r.Commitments[i]
		for _, t := range []*extraction.Term{&cm.Amount, &cm.Covers, &cm.Shortfall} {
			visit(stepCharges, t, nil)
		}
		visit(stepCharges, &cm.Period, billingPeriods)
	}
	for i := range r.Credits {
		cr := &r.Credits[i]
		for _, t := range []*extraction.Term{&cr.Amount, &cr.Kind, &cr.Expiry} {
			visit(stepCharges, t, nil)
		}
	}
	for i := range r.ServiceLevels {
		visit(stepCharges, &r.ServiceLevels[i].Description, nil)
	}
}

func sanitize(r *extraction.Result) {
	eachTerm(r, func(_ string, t *extraction.Term, allowed []string) { sanitizeTerm(t, allowed) })

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

	r.Documents = nonNil(r.Documents)
	r.Charges = nonNil(r.Charges)
	r.Commitments = nonNil(r.Commitments)
	r.Credits = nonNil(r.Credits)
	r.ServiceLevels = nonNil(r.ServiceLevels)
	r.Doubts = nonNil(r.Doubts)
}

func sanitizeTerm(t *extraction.Term, allowed []string) {
	t.Value = strings.TrimSpace(t.Value)
	t.Quote = strings.TrimSpace(t.Quote)
	t.Note = strings.TrimSpace(t.Note)
	t.Options = lo.Filter(lo.Map(t.Options, func(o string, _ int) string { return strings.TrimSpace(o) }),
		func(o string, _ int) bool { return o != "" })
	t.Derived = false

	if len(allowed) > 0 && t.Value != "" {
		if v, ok := matchAllowed(t.Value, allowed); ok {
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

func matchAllowed(v string, allowed []string) (string, bool) {
	for _, a := range allowed {
		if strings.EqualFold(v, a) {
			return a, true
		}
	}
	return "", false
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
