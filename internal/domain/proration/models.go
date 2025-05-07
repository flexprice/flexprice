package proration

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// ProrationAction defines the type of change triggering proration.
type ProrationAction string

const (
	ProrationActionUpgrade        ProrationAction = "upgrade"
	ProrationActionDowngrade      ProrationAction = "downgrade"
	ProrationActionQuantityChange ProrationAction = "quantity_change"
	ProrationActionCancellation   ProrationAction = "cancellation"
	ProrationActionAddItem        ProrationAction = "add_item"
	ProrationActionRemoveItem     ProrationAction = "remove_item"
)

// ProrationStrategy defines how the proration coefficient is calculated.
type ProrationStrategy string

const (
	StrategyDayBased    ProrationStrategy = "day_based"    // Default
	StrategySecondBased ProrationStrategy = "second_based" // Future enhancement
)

// ProrationBehavior defines how proration is applied (e.g., create invoice items).
type ProrationBehavior string

const (
	ProrationBehaviorCreateInvoiceItems ProrationBehavior = "create_invoice_items" // Default: Create credits/charges on invoice
	ProrationBehaviorNone               ProrationBehavior = "none"                 // Calculate but don't apply (e.g., for previews)
)

// BillingMode represents when a subscription is billed.
type BillingMode string

const (
	BillingModeInAdvance BillingMode = "in_advance"
	BillingModeInArrears BillingMode = "in_arrears"
)

// ScheduleType determines when subscription changes take effect.
type ScheduleType string

const (
	ScheduleTypeImmediate    ScheduleType = "immediate"
	ScheduleTypePeriodEnd    ScheduleType = "period_end"
	ScheduleTypeSpecificDate ScheduleType = "specific_date"
)

// TerminationReason represents why a subscription is being terminated.
type TerminationReason string

const (
	TerminationReasonUpgrade      TerminationReason = "upgrade"
	TerminationReasonDowngrade    TerminationReason = "downgrade"
	TerminationReasonCancellation TerminationReason = "cancellation"
	TerminationReasonExpiration   TerminationReason = "expiration"
)

// ProrationParams holds all necessary input for calculating proration.
type ProrationParams struct {
	// Subscription & Line Item Context
	SubscriptionID     string    // ID of the subscription
	LineItemID         string    // ID of the line item being changed (empty for add_item)
	PlanPayInAdvance   bool      // From the subscription's plan
	CurrentPeriodStart time.Time // Start of the current billing period
	CurrentPeriodEnd   time.Time // End of the current billing period

	// Change Details
	Action          types.ProrationAction // Type of change
	OldPriceID      string                // Old price ID (empty for add_item)
	NewPriceID      string                // New price ID (empty for cancellation/remove_item)
	OldQuantity     decimal.Decimal       // Old quantity (zero for add_item)
	NewQuantity     decimal.Decimal       // New quantity (zero for remove_item/cancellation)
	OldPricePerUnit decimal.Decimal       // Price per unit for the old item
	NewPricePerUnit decimal.Decimal       // Price per unit for the new item
	ProrationDate   time.Time             // Effective date/time of the change

	// Configuration & Context
	ProrationBehavior types.ProrationBehavior // How to apply the result
	TerminationReason types.TerminationReason // Required for cancellations/downgrades for credit logic
	ScheduleType      types.ScheduleType      // When the change should take effect
	ScheduleDate      time.Time               // Specific date for scheduled changes (if applicable)
	HasScheduleDate   bool                    // Whether ScheduleDate is set
	CustomerTimezone  string                  // Timezone of the customer

	// Handling Multiple Changes / Credits
	OriginalAmountPaid    decimal.Decimal // Amount originally paid for the item(s) being changed in this period
	PreviousCreditsIssued decimal.Decimal // Sum of credits already issued against OriginalAmountPaid in this period
}

// ProrationLineItem represents a single credit or charge line item.
type ProrationLineItem struct {
	Description string          `json:"description"`
	Amount      decimal.Decimal `json:"amount"`     // Positive for charge, negative for credit
	StartDate   time.Time       `json:"start_date"` // Period this line item covers
	EndDate     time.Time       `json:"end_date"`   // Period this line item covers
	Quantity    decimal.Decimal `json:"quantity"`
	PriceID     string          `json:"price_id"` // Associated price ID if applicable
	IsCredit    bool            `json:"is_credit"`
}

// ProrationResult holds the output of a proration calculation.
type ProrationResult struct {
	CreditItems   []ProrationLineItem   // Items representing credits back to the customer
	ChargeItems   []ProrationLineItem   // Items representing new charges to the customer
	NetAmount     decimal.Decimal       // Net amount (Sum of charges - sum of credits)
	Currency      string                // Currency code
	Action        types.ProrationAction // The action that generated this result
	ProrationDate time.Time             // Effective date used for calculation
	LineItemID    string                // ID of the affected line item (empty for new items)
	IsPreview     bool                  // Indicates if this was calculated for a preview
}
