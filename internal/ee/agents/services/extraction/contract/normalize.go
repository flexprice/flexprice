package contract

import (
	"strings"

	"github.com/flexprice/flexprice/internal/ee/agents/services/extraction"
	"github.com/shopspring/decimal"
)

const (
	overageNameSuffix = " overage"

	notePriceFromTier = "Price per unit read from the only price tier."
	noteSplitFee      = "Split from a usage charge: the contract quotes a fee that includes usage."
	noteSplitOverage  = "Split from the fee that includes this usage: usage above included_quantity is billed at amount per unit."
	noteFeePeriod     = "Read for the charge before it was split into fee and overage; confirm it is how the fee is invoiced."
	noteWaived        = "Waived in the contract."
	noteUsageBank     = "Read as a usage charge with a fee per period and no unit price; moved to commitments as a usage bank."

	waivedMarker = "waive"
)

// normalizeCharges rewrites charge shapes the model often gets wrong:
//   - A usage charge with a single price is priced by amount (per unit), with no tiers or tier_mode.
//   - A usage charge that also quotes a fee for a period (a prepaid allowance with overage) is split
//     into a recurring_fixed charge for the fee and a usage charge for the overage.
func (s *ContractExtractionService) normalizeCharges(charges []extraction.Charge) []extraction.Charge {
	out := make([]extraction.Charge, 0, len(charges))
	for _, ch := range charges {
		ch.Tiers = s.cleanTiers(ch.Tiers)
		if ch.Kind != ChargeKindUsage {
			out = append(out, ch)
			continue
		}

		price, single := s.singlePrice(ch.Tiers)
		if !single {
			if len(ch.Tiers) == 0 {
				ch.TierMode = s.emptyTerm()
			}
			out = append(out, ch)
			continue
		}

		fee := ch.Amount
		hasFee := s.isPositive(fee.Value) && ch.AmountPeriod.IsSet() && ch.AmountPeriod.Value != BillingPeriodOneTime
		overage := ch
		overage.Tiers = []extraction.Tier{}
		overage.TierMode = s.emptyTerm()
		overage.AmountPeriod = s.emptyTerm()
		overage.Amount = s.priceTerm(price, ch)

		if !hasFee {
			out = append(out, overage)
			continue
		}

		out = append(out, s.feeCharge(ch))
		overage.Name.Value = strings.TrimSpace(ch.Name.Value + overageNameSuffix)
		overage.Name.Note = s.appendNote(ch.Name.Note, noteSplitOverage)
		out = append(out, overage)
	}
	return out
}

// moveUsageBanks turns usage charges that quote a fee per period but no unit price (a prepaid usage
// bank that other usage draws down) into commitments, flagged for review.
func (s *ContractExtractionService) moveUsageBanks(r *extraction.Result) {
	charges := make([]extraction.Charge, 0, len(r.Charges))
	for _, ch := range r.Charges {
		if !s.isUsageBank(ch, r.Billing.Currency.Value) {
			charges = append(charges, ch)
			continue
		}
		amount := ch.Amount
		amount.Confidence = min(amount.Confidence, lowLabelConfidence)
		amount.Note = s.appendNote(amount.Note, noteUsageBank)
		r.Commitments = append(r.Commitments, extraction.Commitment{
			Amount:    amount,
			Period:    ch.AmountPeriod,
			Covers:    s.emptyTerm(),
			Shortfall: s.emptyTerm(),
		})
	}
	r.Charges = charges
}

// isUsageBank is a usage charge with a fee per period, no unit price and no allowance other than
// money (a fee that includes minutes or calls is a plain fee, not a bank).
func (s *ContractExtractionService) isUsageBank(ch extraction.Charge, currency string) bool {
	if ch.Kind != ChargeKindUsage || !s.isPositive(ch.Amount.Value) || len(s.cleanTiers(ch.Tiers)) > 0 ||
		!ch.AmountPeriod.IsSet() || ch.AmountPeriod.Value == BillingPeriodOneTime {
		return false
	}
	unit := strings.ToUpper(strings.TrimSpace(ch.Unit.Value))
	return !ch.IncludedQuantity.IsSet() || ch.IncludedQuantity.Value == ch.Amount.Value ||
		unit == "$" || (currency != "" && unit == strings.ToUpper(currency))
}

// feeCharge is the committed fee of a prepaid allowance. It keeps the billing period read for the
// whole charge, capped at low confidence so it is reviewed; its timing is left for review.
func (s *ContractExtractionService) feeCharge(ch extraction.Charge) extraction.Charge {
	period := s.emptyTerm()
	if ch.BillingPeriod.IsSet() {
		period = ch.BillingPeriod
		period.Confidence = min(period.Confidence, lowLabelConfidence)
		period.Note = s.appendNote(period.Note, noteFeePeriod)
	}
	fee := extraction.Charge{
		Kind:               ChargeKindRecurringFixed,
		Name:               ch.Name,
		Amount:             ch.Amount,
		AmountPeriod:       ch.AmountPeriod,
		BillingPeriod:      period,
		BillingTiming:      s.emptyTerm(),
		IncludedQuantity:   s.emptyTerm(),
		Unit:               s.emptyTerm(),
		TierMode:           s.emptyTerm(),
		Tiers:              []extraction.Tier{},
		Trigger:            s.emptyTerm(),
		RevenueRecognition: ch.RevenueRecognition,
	}
	fee.Amount.Note = s.appendNote(fee.Amount.Note, noteSplitFee)
	return fee
}

// markWaived raises the confidence of a zero amount when the contract says the fee is waived,
// either in the charge's own evidence or in the overall billing structure.
func (s *ContractExtractionService) markWaived(r *extraction.Result) {
	structureWaives := s.mentionsWaiver(r.Billing.Structure.Value, r.Billing.Structure.Quote)
	for i := range r.Charges {
		ch := &r.Charges[i]
		amount, err := decimal.NewFromString(ch.Amount.Value)
		if err != nil || !amount.IsZero() {
			continue
		}
		if !structureWaives && !s.mentionsWaiver(ch.Amount.Quote, ch.Name.Quote, ch.Name.Value) {
			continue
		}
		ch.Amount.Confidence = max(ch.Amount.Confidence, highLabelConfidence)
		ch.Amount.Options = []string{}
		if !strings.Contains(ch.Amount.Note, noteWaived) {
			ch.Amount.Note = s.appendNote(ch.Amount.Note, noteWaived)
		}
		if ch.Amount.Page == 0 && r.Billing.Structure.Page > 0 {
			ch.Amount.Page, ch.Amount.Quote, ch.Amount.Boxes = r.Billing.Structure.Page, r.Billing.Structure.Quote, r.Billing.Structure.Boxes
		}
	}
}

func (s *ContractExtractionService) mentionsWaiver(texts ...string) bool {
	for _, t := range texts {
		if strings.Contains(strings.ToLower(t), waivedMarker) {
			return true
		}
	}
	return false
}

// singlePrice reports the per-unit price when the tiers hold exactly one price and no flat fee.
func (s *ContractExtractionService) singlePrice(tiers []extraction.Tier) (string, bool) {
	if len(tiers) != 1 {
		return "", false
	}
	t := tiers[0]
	if t.UnitAmount == "" || s.isPositive(t.FlatAmount) {
		return "", false
	}
	return t.UnitAmount, true
}

// priceTerm carries the tier price as a term. Tiers have no citations of their own, so it borrows
// the evidence of the included allowance (or the charge name), which sits in the same clause.
func (s *ContractExtractionService) priceTerm(price string, ch extraction.Charge) extraction.Term {
	src := ch.IncludedQuantity
	if !src.IsSet() {
		src = ch.Name
	}
	return extraction.Term{
		Value:      price,
		Confidence: src.Confidence,
		Page:       src.Page,
		Quote:      src.Quote,
		Options:    []string{},
		Boxes:      src.Boxes,
		Note:       notePriceFromTier,
	}
}

func (s *ContractExtractionService) cleanTiers(tiers []extraction.Tier) []extraction.Tier {
	out := []extraction.Tier{}
	for _, t := range tiers {
		t = extraction.Tier{
			UpTo:       strings.TrimSpace(t.UpTo),
			UnitAmount: strings.TrimSpace(t.UnitAmount),
			FlatAmount: strings.TrimSpace(t.FlatAmount),
		}
		if t.UpTo == "" && t.UnitAmount == "" && t.FlatAmount == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (s *ContractExtractionService) isPositive(v string) bool {
	d, err := decimal.NewFromString(v)
	return err == nil && d.IsPositive()
}

func (s *ContractExtractionService) emptyTerm() extraction.Term {
	return extraction.Term{Options: []string{}}
}

func (s *ContractExtractionService) appendNote(note, add string) string {
	return strings.TrimSpace(note + " " + add)
}
