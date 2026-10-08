package types

import (
	"time"

	"github.com/shopspring/decimal"
)

// FxConversion is the frozen record of a currency conversion; nil if never converted. A line item
// sets only ChargeCurrency and Source, so the other fields are omitted when zero.
type FxConversion struct {
	// ChargeCurrency is the invoice's original (draft) currency.
	ChargeCurrency string `json:"charge_currency"`
	// BillingCurrency is the currency the invoice was issued in.
	BillingCurrency string `json:"billing_currency,omitzero"`
	// Rate is the frozen rate: billing per 1 charge unit.
	Rate decimal.Decimal `json:"rate,omitzero" swaggertype:"string"`
	// RateID is the fx_rates row used, for reference only; never read again.
	RateID string `json:"rate_id,omitempty"`
	// Scope is where the rate was resolved: subscription, customer or tenant.
	Scope FXRateScope `json:"scope,omitzero"`
	// ConvertedAt is when the conversion ran.
	ConvertedAt time.Time `json:"converted_at,omitzero"`
	// Source holds the original charge-currency amounts, before tax.
	Source FxConversionSource `json:"source"`
}

// FxConversionSource is the pre-conversion charge-currency snapshot used by void and refunds.
type FxConversionSource struct {
	Subtotal                   decimal.Decimal `json:"subtotal" swaggertype:"string"`
	TotalDiscount              decimal.Decimal `json:"total_discount" swaggertype:"string"`
	TotalPrepaidCreditsApplied decimal.Decimal `json:"total_prepaid_credits_applied" swaggertype:"string"`
	Net                        decimal.Decimal `json:"net" swaggertype:"string"`
}
