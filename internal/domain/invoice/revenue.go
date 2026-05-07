package invoice

import (
	"time"

	"github.com/shopspring/decimal"
)

// RevenueByCustomerRow represents a single row from the revenue aggregation query,
// grouped by customer_id and price_type.
type RevenueByCustomerRow struct {
	CustomerID string
	PriceType  string // "USAGE" or "FIXED"
	Amount     decimal.Decimal
}

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
