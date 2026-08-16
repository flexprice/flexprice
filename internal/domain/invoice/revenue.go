package invoice

import (
	"time"

	"github.com/shopspring/decimal"
)

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
