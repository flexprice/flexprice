package billingmatrix

import (
	"github.com/shopspring/decimal"
)

// Billing models, as the API spells them.
const (
	modelFlatFee = "FLAT_FEE"
	modelPackage = "PACKAGE"
	modelTiered  = "TIERED"

	tierVolume = "VOLUME"
	tierSlab   = "SLAB"
)

type tier struct {
	upTo       *int64 // nil = unbounded last tier; inclusive
	unitAmount decimal.Decimal
	flatAmount decimal.Decimal
}

// pricing is a fixed price's money model, independent of its cadence.
type pricing struct {
	model    string
	amount   decimal.Decimal // flat fee per unit, or price per package
	divideBy int64           // package size
	tierMode string
	tiers    []tier
}

func flatPricing(amount string) pricing {
	return pricing{model: modelFlatFee, amount: decimal.RequireFromString(amount)}
}

// cost is the full-period cost for qty units, before proration.
func (p pricing) cost(qty decimal.Decimal) decimal.Decimal {
	if qty.IsZero() {
		return decimal.Zero
	}
	switch p.model {
	case modelPackage:
		if p.divideBy <= 0 {
			return decimal.Zero
		}
		return p.amount.Mul(qty.Div(decimal.NewFromInt(p.divideBy)).Ceil())
	case modelTiered:
		if p.tierMode == tierVolume {
			return p.volumeCost(qty)
		}
		return p.slabCost(qty)
	}
	return p.amount.Mul(qty)
}

func (p pricing) volumeCost(qty decimal.Decimal) decimal.Decimal {
	for _, t := range p.tiers {
		if t.upTo == nil || qty.LessThanOrEqual(decimal.NewFromInt(*t.upTo)) {
			return t.unitAmount.Mul(qty).Add(t.flatAmount)
		}
	}
	last := p.tiers[len(p.tiers)-1]
	return last.unitAmount.Mul(qty).Add(last.flatAmount)
}

func (p pricing) slabCost(qty decimal.Decimal) decimal.Decimal {
	total, remaining, floor := decimal.Zero, qty, decimal.Zero
	for _, t := range p.tiers {
		inTier := remaining
		if t.upTo != nil {
			capacity := decimal.NewFromInt(*t.upTo).Sub(floor)
			if remaining.GreaterThan(capacity) {
				inTier = capacity
			}
			floor = decimal.NewFromInt(*t.upTo)
		}
		total = total.Add(t.unitAmount.Mul(inTier).Add(t.flatAmount))
		remaining = remaining.Sub(inTier)
		if !remaining.IsPositive() {
			break
		}
	}
	return total
}
