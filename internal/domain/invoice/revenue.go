package invoice

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// LineNets is each line's revenue keyed by line ID: its amount less its line and invoice-level discounts, less its share
// of the invoice's inclusive tax. The share is pro rata by net, with the rounding residual on the line with the largest
// absolute net (ties by ID), so the result does not depend on line order; prepaid credits are settlement, not deducted.
// e.g. lines netting 60 and 40 with 12 inclusive tax → 52.80 and 35.20.
func (i *Invoice) LineNets(taxes []*taxapplied.TaxApplied) map[string]decimal.Decimal {
	nets := make(map[string]decimal.Decimal, len(i.LineItems))
	total := decimal.Zero
	residualLine := ""
	for _, li := range i.LineItems {
		net := li.Amount.Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount)
		nets[li.ID] = net
		total = total.Add(net)

		largest := nets[residualLine].Abs()
		if residualLine == "" || net.Abs().GreaterThan(largest) || (net.Abs().Equal(largest) && li.ID < residualLine) {
			residualLine = li.ID
		}
	}

	inclusiveTax := decimal.Zero
	for _, t := range taxes {
		if t.EntityType == types.TaxRateEntityTypeInvoice && t.EntityID == i.ID &&
			t.TaxBehavior == types.TaxBehaviorInclusive && !t.IsReversal() {
			inclusiveTax = inclusiveTax.Add(t.TaxAmount)
		}
	}
	if inclusiveTax.IsZero() || total.IsZero() {
		return nets
	}

	// e.g. 12 tax over nets 60 and 40: the 40 line takes round(12 × 40/100) = 4.80, the largest line the remaining 7.20
	allocated := decimal.Zero
	for _, li := range i.LineItems {
		if li.ID == residualLine {
			continue
		}
		share := types.RoundToCurrencyPrecision(inclusiveTax.Mul(nets[li.ID]).Div(total), i.Currency)
		nets[li.ID] = nets[li.ID].Sub(share)
		allocated = allocated.Add(share)
	}
	nets[residualLine] = nets[residualLine].Sub(inclusiveTax.Sub(allocated))

	return nets
}

// RevenueByCustomerRow represents a single row from the revenue aggregation query,
// grouped by customer_id, price_type, and currency.
type RevenueByCustomerRow struct {
	CustomerID string
	PriceType  string // "USAGE" or "FIXED"
	Currency   string
	Amount     decimal.Decimal
}

// BilledAmounts is charges/credits already invoiced for a subscription line item
// (non-negative). Used to cap proration credits so we never credit more than billed.
type BilledAmounts struct {
	charged  decimal.Decimal
	credited decimal.Decimal
}

func NewBilledAmounts(charged, credited decimal.Decimal) *BilledAmounts {
	return &BilledAmounts{charged: charged, credited: credited}
}

func (b *BilledAmounts) Charged() decimal.Decimal { return b.charged }

func (b *BilledAmounts) Credited() decimal.Decimal { return b.credited }

// VoiceMinutesRow represents a single row from the voice minutes aggregation query,
// grouped by customer_id.
type VoiceMinutesRow struct {
	CustomerID string
	UsageMs    decimal.Decimal // raw milliseconds from SUM(quantity)
}

// RevenueTimeSeriesRow is a revenue aggregate for one time bucket and price type.
type RevenueTimeSeriesRow struct {
	WindowStart time.Time
	PriceType   string // "USAGE" or "FIXED"
	Amount      decimal.Decimal
}

// VoiceMinutesTimeSeriesRow is voice usage (ms) for one time bucket.
type VoiceMinutesTimeSeriesRow struct {
	WindowStart time.Time
	UsageMs     decimal.Decimal
}
