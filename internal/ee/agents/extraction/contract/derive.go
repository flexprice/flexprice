package contract

import (
	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const dateLayout = "2006-01-02"

func derive(r *extraction.Result) *extraction.Derived {
	d := &extraction.Derived{InvoiceSchedules: []extraction.InvoiceSchedule{}}

	start, startOK := parseDate(r.Term.EffectiveDate.Value)
	months, monthsOK := parseInt(r.Term.TermMonths.Value)
	if !startOK || !monthsOK || months <= 0 {
		return d
	}
	end := addMonths(start, months).AddDate(0, 0, -1)
	d.EndDate = derivedTerm(end.Format(dateLayout), minConfidence(r.Term.EffectiveDate, r.Term.TermMonths))

	if days, ok := parseInt(r.Term.NoticeDays.Value); ok && days > 0 {
		d.NoticeDeadline = derivedTerm(end.AddDate(0, 0, -days).Format(dateLayout),
			minConfidence(r.Term.EffectiveDate, r.Term.TermMonths, r.Term.NoticeDays))
	}

	for i, ch := range r.Charges {
		if s, ok := invoiceSchedule(i, ch, start, months); ok {
			d.InvoiceSchedules = append(d.InvoiceSchedules, s)
		}
	}
	return d
}

func invoiceSchedule(index int, ch extraction.Charge, start time.Time, termMonths int) (extraction.InvoiceSchedule, bool) {
	if ch.Kind != ChargeKindRecurringFixed || ch.BillingTiming.Value == BillingTimingArrears {
		return extraction.InvoiceSchedule{}, false
	}
	every, ok := periodMonths[ch.BillingPeriod.Value]
	if !ok {
		return extraction.InvoiceSchedule{}, false
	}
	quotedOver, ok := periodMonths[ch.AmountPeriod.Value]
	if !ok {
		quotedOver = every
	}
	amount, err := decimal.NewFromString(ch.Amount.Value)
	if err != nil || !amount.IsPositive() {
		return extraction.InvoiceSchedule{}, false
	}

	dates := []string{}
	for m := 0; m < termMonths; m += every {
		dates = append(dates, addMonths(start, m).Format(dateLayout))
	}
	perInvoice := amount.Mul(decimal.NewFromInt(int64(every))).Div(decimal.NewFromInt(int64(quotedOver)))
	return extraction.InvoiceSchedule{
		ChargeIndex:      index,
		ChargeName:       ch.Name.Value,
		AmountPerInvoice: perInvoice.Round(2).StringFixed(2),
		Dates:            dates,
	}, true
}

func addMonths(t time.Time, months int) time.Time {
	first := time.Date(t.Year(), t.Month()+time.Month(months), 1, 0, 0, 0, 0, t.Location())
	lastDay := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(t.Day(), lastDay), 0, 0, 0, 0, t.Location())
}

func derivedTerm(value string, confidence int) extraction.Term {
	return extraction.Term{Value: value, Confidence: confidence, Options: []string{}, Derived: true}
}

func minConfidence(terms ...extraction.Term) int {
	c := maxConfidence
	for _, t := range terms {
		c = min(c, t.Confidence)
	}
	return c
}

func parseDate(v string) (time.Time, bool) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(v))
	return t, err == nil
}

func parseInt(v string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	return n, err == nil
}
