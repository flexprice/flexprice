package subscription

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
)

// SubscriptionChange represents a subscription plan change operation
type SubscriptionChange struct {
	// ID is the unique identifier for the subscription change
	ID string `json:"id"`

	// SubscriptionID is the ID of the subscription being changed
	SubscriptionID string `json:"subscription_id"`

	// TargetPlanID is the ID of the new plan to change to
	TargetPlanID string `json:"target_plan_id"`

	// ProrationBehavior controls how proration is handled
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior"`

	// EffectiveDate is when the change takes effect
	EffectiveDate time.Time `json:"effective_date"`

	// BillingCycleAnchor controls how billing cycle is handled
	BillingCycleAnchor types.BillingCycleAnchor `json:"billing_cycle_anchor"`

	// Metadata contains additional key-value pairs
	Metadata types.Metadata `json:"metadata,omitempty"`

	// Base model fields
	types.BaseModel
}

// SubscriptionChangePreview represents the preview of a subscription change
type SubscriptionChangePreview struct {
	// SubscriptionID is the ID of the subscription being changed
	SubscriptionID string `json:"subscription_id"`

	// CurrentPlanID is the current plan ID
	CurrentPlanID string `json:"current_plan_id"`

	// TargetPlanID is the target plan ID
	TargetPlanID string `json:"target_plan_id"`

	// ChangeType indicates upgrade, downgrade, or lateral
	ChangeType types.SubscriptionChangeType `json:"change_type"`

	// ProrationDetails contains proration calculations
	ProrationDetails *SubscriptionProrationPreview `json:"proration_details,omitempty"`

	// EffectiveDate when the change would take effect
	EffectiveDate time.Time `json:"effective_date"`

	// NewBillingCycle information
	NewBillingCycle *BillingCyclePreview `json:"new_billing_cycle,omitempty"`

	// ImmediateInvoiceAmount is the amount that would be invoiced immediately
	ImmediateInvoiceAmount *InvoiceAmountPreview `json:"immediate_invoice_amount,omitempty"`

	// NextInvoiceAmount is how the next invoice would be affected
	NextInvoiceAmount *InvoiceAmountPreview `json:"next_invoice_amount,omitempty"`

	// Warnings about the change
	Warnings []string `json:"warnings,omitempty"`
}

// SubscriptionProrationPreview contains proration calculation details
type SubscriptionProrationPreview struct {
	// CreditAmount from the current subscription
	CreditAmount string `json:"credit_amount"`

	// ChargeAmount for the new subscription
	ChargeAmount string `json:"charge_amount"`

	// NetAmount is the net change
	NetAmount string `json:"net_amount"`

	// Currency for all amounts
	Currency string `json:"currency"`

	// ProrationDate used for calculations
	ProrationDate time.Time `json:"proration_date"`

	// DaysUsed in current period
	DaysUsed int `json:"days_used"`

	// DaysRemaining in current period
	DaysRemaining int `json:"days_remaining"`
}

// BillingCyclePreview contains new billing cycle information
type BillingCyclePreview struct {
	// NewPeriodStart when the new billing period starts
	NewPeriodStart time.Time `json:"new_period_start"`

	// NewPeriodEnd when the new billing period ends
	NewPeriodEnd time.Time `json:"new_period_end"`

	// NewBillingAnchor for future billing cycles
	NewBillingAnchor time.Time `json:"new_billing_anchor"`

	// BillingCadence of the new plan
	BillingCadence types.BillingCadence `json:"billing_cadence"`

	// BillingPeriod of the new plan
	BillingPeriod types.BillingPeriod `json:"billing_period"`

	// BillingPeriodCount of the new plan
	BillingPeriodCount int `json:"billing_period_count"`
}

// InvoiceAmountPreview contains invoice amount preview information
type InvoiceAmountPreview struct {
	// Subtotal before taxes
	Subtotal string `json:"subtotal"`

	// TaxAmount total tax
	TaxAmount string `json:"tax_amount"`

	// Total including taxes
	Total string `json:"total"`

	// Currency for all amounts
	Currency string `json:"currency"`

	// LineItems preview
	LineItems []LineItemPreview `json:"line_items,omitempty"`
}

// LineItemPreview contains line item preview information
type LineItemPreview struct {
	// Description of the line item
	Description string `json:"description"`

	// Amount for the line item
	Amount string `json:"amount"`

	// Quantity for the line item
	Quantity string `json:"quantity"`

	// IsProration indicates if this is a proration line item
	IsProration bool `json:"is_proration"`

	// PeriodStart for the line item
	PeriodStart *time.Time `json:"period_start,omitempty"`

	// PeriodEnd for the line item
	PeriodEnd *time.Time `json:"period_end,omitempty"`
}

// SubscriptionChangeResult represents the result of executing a subscription change
type SubscriptionChangeResult struct {
	// OldSubscriptionID is the ID of the archived subscription
	OldSubscriptionID string `json:"old_subscription_id"`

	// NewSubscriptionID is the ID of the new subscription
	NewSubscriptionID string `json:"new_subscription_id"`

	// ChangeType that was executed
	ChangeType types.SubscriptionChangeType `json:"change_type"`

	// InvoiceID of the immediate invoice (if generated)
	InvoiceID *string `json:"invoice_id,omitempty"`

	// ProrationApplied details
	ProrationApplied *SubscriptionProrationPreview `json:"proration_applied,omitempty"`

	// EffectiveDate when the change took effect
	EffectiveDate time.Time `json:"effective_date"`
}
