package contract

import (
	"strconv"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/ee/agents/extraction"
	contractagent "github.com/flexprice/flexprice/internal/ee/agents/extraction/contract"
	"github.com/shopspring/decimal"
)

const dateLayout = "2006-01-02"

func (c *contractIngestService) derive(r *extraction.Result) *extraction.Derived {
	d := &extraction.Derived{InvoiceSchedules: []extraction.InvoiceSchedule{}}

	start, startOK := c.parseDate(r.Term.EffectiveDate.Value)
	months, monthsOK := c.parseInt(r.Term.TermMonths.Value)
	if !startOK || !monthsOK || months <= 0 {
		return d
	}
	end := c.addMonths(start, months).AddDate(0, 0, -1)
	d.EndDate = c.derivedTerm(end.Format(dateLayout), c.minConfidence(r.Term.EffectiveDate, r.Term.TermMonths))

	if days, ok := c.parseInt(r.Term.NoticeDays.Value); ok && days > 0 {
		d.NoticeDeadline = c.derivedTerm(end.AddDate(0, 0, -days).Format(dateLayout), c.minConfidence(r.Term.EffectiveDate, r.Term.TermMonths, r.Term.NoticeDays))
	}

	for i, ch := range r.Charges {
		if s, ok := c.invoiceSchedule(i, ch, start, months); ok {
			d.InvoiceSchedules = append(d.InvoiceSchedules, s)
		}
	}
	return d
}

func (c *contractIngestService) invoiceSchedule(index int, ch extraction.Charge, start time.Time, termMonths int) (extraction.InvoiceSchedule, bool) {
	if ch.Kind != contractagent.ChargeKindRecurringFixed || ch.BillingTiming.Value == contractagent.BillingTimingArrears {
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
		dates = append(dates, c.addMonths(start, m).Format(dateLayout))
	}
	perInvoice := amount.Mul(decimal.NewFromInt(int64(every))).Div(decimal.NewFromInt(int64(quotedOver)))
	return extraction.InvoiceSchedule{
		ChargeIndex:      index,
		ChargeName:       ch.Name.Value,
		AmountPerInvoice: perInvoice.Round(2).StringFixed(2),
		Dates:            dates,
	}, true
}

func (c *contractIngestService) addMonths(t time.Time, months int) time.Time {
	first := time.Date(t.Year(), t.Month()+time.Month(months), 1, 0, 0, 0, 0, t.Location())
	lastDay := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(t.Day(), lastDay), 0, 0, 0, 0, t.Location())
}

func (c *contractIngestService) derivedTerm(value string, confidence int) extraction.Term {
	return extraction.Term{Value: value, Confidence: confidence, Options: []string{}, Derived: true}
}

func (c *contractIngestService) minConfidence(terms ...extraction.Term) int {
	lowest := maxConfidence
	for _, t := range terms {
		lowest = min(lowest, t.Confidence)
	}
	return lowest
}

func (c *contractIngestService) parseDate(v string) (time.Time, bool) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(v))
	return t, err == nil
}

func (c *contractIngestService) parseInt(v string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	return n, err == nil
}
