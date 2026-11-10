package invoice

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/taxapplied"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/utils"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// LineNets is each line's net amount less its pro rata share of the invoice's inclusive tax, keyed by line ID.
// e.g. lines netting 60 and 40 with 12 inclusive tax → 52.80 and 35.20.
func (i *Invoice) LineNets(taxes []*taxapplied.TaxApplied) map[string]decimal.Decimal {
	nets := lo.SliceToMap(i.LineItems, func(li *InvoiceLineItem) (string, decimal.Decimal) { return li.ID, li.NetAmount() })

	inclusiveTax := decimal.Zero
	for _, t := range taxes {
		if t.EntityType == types.TaxRateEntityTypeInvoice && t.EntityID == i.ID &&
			t.TaxBehavior == types.TaxBehaviorInclusive && !t.IsReversal() {
			inclusiveTax = inclusiveTax.Add(t.TaxAmount)
		}
	}
	if inclusiveTax.IsZero() {
		return nets
	}

	shares := utils.AllocateProRata(nets, inclusiveTax, types.GetCurrencyPrecision(i.Currency))
	return lo.MapValues(nets, func(net decimal.Decimal, id string) decimal.Decimal { return net.Sub(shares[id]) })
}

// NetAmount is the line's amount less its line and invoice-level discounts, before tax.
func (li *InvoiceLineItem) NetAmount() decimal.Decimal {
	return li.Amount.Sub(li.LineItemDiscount).Sub(li.InvoiceLevelDiscount)
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
