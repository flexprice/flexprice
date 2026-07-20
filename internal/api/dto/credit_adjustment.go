package dto

import "github.com/shopspring/decimal"

// CreditAdjustmentResult holds the result of applying credit adjustments to an invoice.
type CreditAdjustmentResult struct {
	// TotalPrepaidCreditsApplied is the invoice's cumulative prepaid total after this call
	// (includes prior adjustments, not just what this call applied).
	TotalPrepaidCreditsApplied decimal.Decimal `json:"total_prepaid_credits_applied" swaggertype:"string"`
	// AmountApplied is how much was newly applied in this call (wallet debit / delta).
	AmountApplied decimal.Decimal `json:"amount_applied" swaggertype:"string"`
	Currency      string          `json:"currency"`
}
