package dto

import "github.com/shopspring/decimal"

// CreditAdjustmentResult holds the result of applying credit adjustments to an invoice
type CreditAdjustmentResult struct {
	TotalPrepaidCreditsApplied decimal.Decimal `json:"total_prepaid_credits_applied" swaggertype:"string"`
	Currency                   string          `json:"currency"`
}
