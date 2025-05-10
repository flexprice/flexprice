package proration

import (
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
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
	OriginalAmountPaid    decimal.Decimal         // Amount originally paid for the item(s) being changed in this period
	PreviousCreditsIssued decimal.Decimal         // Sum of credits already issued against OriginalAmountPaid in this period
	ProrationStrategy     types.ProrationStrategy // Strategy to use for proration
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

// SubscriptionProrationParams contains all necessary information for subscription-level proration
type SubscriptionProrationParams struct {
	Subscription     *subscription.Subscription
	LineItems        []*subscription.SubscriptionLineItem
	Prices           map[string]*price.Price // Map of priceID to price
	ProrationMode    types.ProrationMode
	BillingCycle     types.BillingCycle
	StartDate        time.Time
	BillingAnchor    time.Time
	CustomerTimezone string
}

// SubscriptionProrationResult contains the results of subscription-level proration
type SubscriptionProrationResult struct {
	TotalProrationAmount decimal.Decimal
	LineItemResults      map[string]*ProrationResult // Map of lineItemID to its proration result
	InvoiceID            string                      // ID of the invoice created/updated with proration items
}
