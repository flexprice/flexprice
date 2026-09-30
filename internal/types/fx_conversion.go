package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// FxConversion is the frozen record of a fiat→fiat conversion applied to an invoice at
// finalization. Nil on invoices that were never converted. Written once, in the same
// transaction as the converted amounts, and never changed afterwards.
type FxConversion struct {
	// ChargeCurrency is the invoice's original (draft) currency.
	ChargeCurrency string `json:"charge_currency"`
	// BillingCurrency is the currency the invoice was issued in.
	BillingCurrency string `json:"billing_currency"`
	// Rate is the frozen rate: billing per 1 charge unit.
	Rate decimal.Decimal `json:"rate" swaggertype:"string"`
	// RateID is the fx_rates row used, for reference only; never read again.
	RateID string `json:"rate_id,omitempty"`
	// Scope is where the rate was resolved: subscription, customer or tenant.
	Scope string `json:"scope"`
	// ConvertedAt is when the conversion ran.
	ConvertedAt time.Time `json:"converted_at"`
	// Source holds the original charge-currency amounts, before tax.
	Source FxConversionSource `json:"source"`
	// RoundingAdjustment is the residual added to the largest line so lines sum to the net.
	RoundingAdjustment decimal.Decimal `json:"rounding_adjustment" swaggertype:"string"`
	// RoundingLineItemID is the line that absorbed the rounding residual.
	RoundingLineItemID string `json:"rounding_line_item_id,omitempty"`
}

// FxConversionSource is the pre-conversion charge-currency snapshot used by void and refunds.
type FxConversionSource struct {
	Subtotal                   decimal.Decimal `json:"subtotal" swaggertype:"string"`
	TotalDiscount              decimal.Decimal `json:"total_discount" swaggertype:"string"`
	TotalPrepaidCreditsApplied decimal.Decimal `json:"total_prepaid_credits_applied" swaggertype:"string"`
	Net                        decimal.Decimal `json:"net" swaggertype:"string"`
}
