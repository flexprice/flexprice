package contract

import (
	"context"
	"strconv"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	contractagent "github.com/flexprice/flexprice/internal/ee/agents/services/extraction/contract"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// DeriveTerms recomputes the derived values from the saved review with unsaved edits applied.
// Nothing is saved.
func (c *contractIngestService) DeriveTerms(ctx context.Context, contractID string, req *dto.DeriveContractTermsRequest) (*extraction.Derived, error) {
	_, _, doc, err := c.loadReview(ctx, contractID)
	if err != nil {
		return nil, err
	}
	if err := c.applyEdits(doc.Result, req); err != nil {
		return nil, err
	}
	return c.derive(doc.Result), nil
}

func (c *contractIngestService) applyEdits(r *extraction.Result, req *dto.DeriveContractTermsRequest) error {
	if req == nil {
		return nil
	}
	if len(req.Charges) > len(r.Charges) {
		return ierr.NewError("more charges than the contract has").
			WithHint("Send one entry per charge in extraction.result.charges, in the same order").
			WithReportableDetails(map[string]any{"charges": len(r.Charges)}).
			Mark(ierr.ErrValidation)
	}
	if t := req.Term; t != nil {
		if err := c.applyEdit(&r.Term.EffectiveDate, t.EffectiveDate, "term.effective_date", c.validDate); err != nil {
			return err
		}
		if err := c.applyEdit(&r.Term.TermMonths, t.TermMonths, "term.term_months", c.validCount); err != nil {
			return err
		}
		if err := c.applyEdit(&r.Term.NoticeDays, t.NoticeDays, "term.notice_days", c.validCount); err != nil {
			return err
		}
	}
	for i, in := range req.Charges {
		ch, p := &r.Charges[i], "charges["+strconv.Itoa(i)+"]."
		if err := c.applyEdit(&ch.Amount, in.Amount, p+"amount", c.validAmount); err != nil {
			return err
		}
		if err := c.applyEdit(&ch.AmountPeriod, in.AmountPeriod, p+"amount_period", c.validPeriod); err != nil {
			return err
		}
		if err := c.applyEdit(&ch.BillingPeriod, in.BillingPeriod, p+"billing_period", c.validPeriod); err != nil {
			return err
		}
		if err := c.applyEdit(&ch.BillingTiming, in.BillingTiming, p+"billing_timing", c.validTiming); err != nil {
			return err
		}
	}
	return nil
}

// applyEdit replaces a term's value with the reviewer's, who is taken to be certain of it.
// An empty value clears the term.
func (c *contractIngestService) applyEdit(t *extraction.Term, value *string, field string, valid func(string) bool) error {
	if value == nil {
		return nil
	}
	if *value != "" && !valid(*value) {
		return ierr.NewError("invalid term value").
			WithHintf("%s has an invalid value", field).
			WithReportableDetails(map[string]any{"field": field, "value": *value}).
			Mark(ierr.ErrValidation)
	}
	t.Value, t.Confidence, t.Options = *value, maxConfidence, []string{}
	return nil
}

func (c *contractIngestService) validDate(v string) bool {
	_, ok := c.parseDate(v)
	return ok
}

func (c *contractIngestService) validCount(v string) bool {
	n, ok := c.parseInt(v)
	return ok && n >= 0
}

func (c *contractIngestService) validAmount(v string) bool {
	d, err := decimal.NewFromString(v)
	return err == nil && !d.IsNegative()
}

func (c *contractIngestService) validPeriod(v string) bool {
	return lo.Contains([]string{
		contractagent.BillingPeriodMonthly, contractagent.BillingPeriodQuarterly, contractagent.BillingPeriodHalfYearly,
		contractagent.BillingPeriodAnnual, contractagent.BillingPeriodWeekly, contractagent.BillingPeriodDaily,
		contractagent.BillingPeriodOneTime,
	}, v)
}

func (c *contractIngestService) validTiming(v string) bool {
	return v == contractagent.BillingTimingAdvance || v == contractagent.BillingTimingArrears
}
