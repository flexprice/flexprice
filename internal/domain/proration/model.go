package proration

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// ProrationParams contains the parameters for calculating a proration
type ProrationParams struct {
	LineItemID        string                  `json:"line_item_id,omitempty"`   // ID of the line item being changed (can be empty for add_item action)
	OldPriceID        *string                 `json:"old_price_id,omitempty"`   // Old price ID (nil for add_item)
	NewPriceID        *string                 `json:"new_price_id,omitempty"`   // New price ID (nil for cancellation or remove_item)
	OldQuantity       *decimal.Decimal        `json:"old_quantity,omitempty"`   // Old quantity (nil for add_item)
	NewQuantity       *decimal.Decimal        `json:"new_quantity,omitempty"`   // New quantity
	ProrationDate     time.Time               `json:"proration_date,omitempty"` // When the proration takes effect
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`
	ProrationStrategy types.ProrationStrategy `json:"proration_strategy,omitempty"`
	Action            types.ProrationAction   `json:"action,omitempty"`
}

func GetDefaultProrationParams() *ProrationParams {
	return &ProrationParams{
		ProrationDate:     time.Now().UTC(),
		ProrationBehavior: types.ProrationBehaviorAlwaysInvoice,
		ProrationStrategy: types.ProrationStrategyDayBased,
	}
}

// ProrationResult represents the result of a proration calculation
type ProrationResult struct {
	Credits       []ProrationLineItem   // Credit line items
	Charges       []ProrationLineItem   // Charge line items
	NetAmount     decimal.Decimal       // Net amount (positive means customer owes, negative means refund/credit)
	Currency      string                // Currency code
	Action        types.ProrationAction // The type of action that generated this proration
	ProrationDate time.Time             // Effective date for the proration
	LineItemID    string                // ID of the affected line item (empty for new items)
	IsPreview     bool                  // Whether this is a preview or actual proration
}

// ProrationLineItem represents a single line item in a proration result
type ProrationLineItem struct {
	Description    string                // Description of the line item
	Amount         decimal.Decimal       // Amount (always positive)
	Type           types.TransactionType // Credit or Charge
	PriceID        string                // Associated price ID
	Quantity       decimal.Decimal       // Quantity
	UnitAmount     decimal.Decimal       // Unit amount
	PeriodStart    time.Time             // Start of the applicable period
	PeriodEnd      time.Time             // End of the applicable period
	PlanChangeType types.PlanChangeType  // Whether this is an upgrade or downgrade
}
