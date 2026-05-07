package dto

import (
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// SubModifyInheritanceRequest is the payload for adding
// inherited child subscriptions to a parent subscription.
type SubModifyInheritanceRequest struct {
	ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`
}

func (r *SubModifyInheritanceRequest) Validate() error {
	if len(r.ExternalCustomerIDsToInheritSubscription) == 0 {
		return ierr.NewError("at least one external customer ID is required").
			WithHint("Provide external_customer_ids_to_inherit_subscription with at least one non-empty value").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// LineItemQuantityChange describes a quantity change for a single line item.
type LineItemQuantityChange struct {
	ID       string          `json:"id" binding:"required"`
	Quantity decimal.Decimal `json:"quantity" swaggertype:"string" binding:"required"`
	// EffectiveDate is when the quantity change takes effect.
	// If omitted, the change is effective immediately (now).
	EffectiveDate *time.Time `json:"effective_date,omitempty"`
}

// SubModifyQuantityChangeRequest is the payload for mid-cycle seat/quantity changes.
type SubModifyQuantityChangeRequest struct {
	LineItems []LineItemQuantityChange `json:"line_items" binding:"required,min=1"`
}

func (r *SubModifyQuantityChangeRequest) Validate() error {
	if len(r.LineItems) == 0 {
		return ierr.NewError("at least one line item is required").
			WithHint("Provide line_items with at least one entry").
			Mark(ierr.ErrValidation)
	}
	for _, li := range r.LineItems {
		if li.ID == "" {
			return ierr.NewError("line item ID is required").
				WithHint("Each line_item entry must have a non-empty id").
				Mark(ierr.ErrValidation)
		}
		if li.Quantity.LessThanOrEqual(decimal.Zero) {
			return ierr.NewError("quantity must be positive").
				WithHint("Each line_item quantity must be greater than zero").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// SubscriptionModifyType identifies the kind of modification.
type SubscriptionModifyType string

const (
	SubscriptionModifyTypeInheritance    SubscriptionModifyType = "inheritance"
	SubscriptionModifyTypeQuantityChange SubscriptionModifyType = "quantity_change"
)

// ExecuteSubscriptionModifyRequest is the unified body for
// POST /subscriptions/:id/modify/execute and /modify/preview.
// Exactly one of InheritanceParams or QuantityChangeParams must be set.
type ExecuteSubscriptionModifyRequest struct {
	Type                 SubscriptionModifyType          `json:"type" binding:"required"`
	InheritanceParams    *SubModifyInheritanceRequest    `json:"inheritance_params,omitempty"`
	QuantityChangeParams *SubModifyQuantityChangeRequest `json:"quantity_change_params,omitempty"`
}

func (r *ExecuteSubscriptionModifyRequest) Validate() error {
	switch r.Type {
	case SubscriptionModifyTypeInheritance:
		if r.InheritanceParams == nil {
			return ierr.NewError("inheritance_params is required for type 'inheritance'").
				Mark(ierr.ErrValidation)
		}
		return r.InheritanceParams.Validate()
	case SubscriptionModifyTypeQuantityChange:
		if r.QuantityChangeParams == nil {
			return ierr.NewError("quantity_change_params is required for type 'quantity_change'").
				Mark(ierr.ErrValidation)
		}
		return r.QuantityChangeParams.Validate()
	default:
		return ierr.NewError("unknown modification type: " + string(r.Type)).
			WithHint("Valid values: inheritance, quantity_change").
			Mark(ierr.ErrValidation)
	}
}

// ChangedLineItemAction describes how a subscription line item changed.
// @Description created | updated | ended
type ChangedLineItemAction string

const (
	ChangedLineItemActionCreated ChangedLineItemAction = "created"
	ChangedLineItemActionUpdated ChangedLineItemAction = "updated"
	ChangedLineItemActionEnded   ChangedLineItemAction = "ended"
)

// ChangedSubscriptionAction describes how a subscription row changed.
// @Description created | updated
type ChangedSubscriptionAction string

const (
	ChangedSubscriptionActionCreated ChangedSubscriptionAction = "created"
	ChangedSubscriptionActionUpdated ChangedSubscriptionAction = "updated"
)

// ChangedInvoiceAction classifies invoice-side effects from a modification.
// @Description created (proration invoice) | wallet_credit (downgrade credit)
type ChangedInvoiceAction string

const (
	ChangedInvoiceActionCreated      ChangedInvoiceAction = "created"
	ChangedInvoiceActionWalletCredit ChangedInvoiceAction = "wallet_credit"
)

// ChangedInvoiceStatus is the high-level status for ChangedInvoice.
// Values "preview" and "issued" are used for preview payloads and completed wallet credits.
// Proration invoice results use the same strings as types.PaymentStatus (e.g. SUCCEEDED, PENDING, FAILED).
// @Description preview | issued | INITIATED | PENDING | PROCESSING | SUCCEEDED | OVERPAID | FAILED | REFUNDED | PARTIALLY_REFUNDED
type ChangedInvoiceStatus string

const (
	ChangedInvoiceStatusPreview      ChangedInvoiceStatus = "preview"
	ChangedInvoiceStatusWalletIssued ChangedInvoiceStatus = "issued"
)

// ChangedInvoiceStatusFromPaymentStatus maps a persisted invoice payment status for execute responses.
func ChangedInvoiceStatusFromPaymentStatus(ps types.PaymentStatus) ChangedInvoiceStatus {
	return ChangedInvoiceStatus(ps)
}

// ChangedLineItem describes a subscription line item that was created, updated, or ended.
type ChangedLineItem struct {
	ID           string                `json:"id"`
	PriceID      string                `json:"price_id"`
	Quantity     decimal.Decimal       `json:"quantity" swaggertype:"string"`
	StartDate    *time.Time            `json:"start_date,omitempty"`
	EndDate      *time.Time            `json:"end_date,omitempty"`
	ChangeAction ChangedLineItemAction `json:"change_action" enums:"created,updated,ended"`
}

// ChangedSubscription describes a subscription that was created or updated.
type ChangedSubscription struct {
	ID     string                    `json:"id"`
	Action ChangedSubscriptionAction `json:"action"`
	Status types.SubscriptionStatus  `json:"status"`
}

// ChangedInvoice describes a proration invoice or wallet credit from a modification.
type ChangedInvoice struct {
	ID string `json:"id"`
	// Action is created for a proration charge invoice, wallet_credit for downgrade credit.
	Action ChangedInvoiceAction `json:"action"`
	// Status is preview (dry-run), issued (wallet credit applied), or a PaymentStatus string for real invoices.
	Status ChangedInvoiceStatus `json:"status"`
	// Invoice is set for proration charges: preview returns a synthetic invoice; execute returns the persisted invoice when created.
	Invoice *InvoiceResponse `json:"invoice,omitempty"`
	// WalletTransaction is set for downgrade wallet credits: preview is synthetic; execute returns the transaction from the top-up.
	WalletTransaction *WalletTransactionResponse `json:"wallet_transaction,omitempty"`
}

// ChangedResources is the Orb-inspired envelope for all mutation side-effects.
type ChangedResources struct {
	LineItems     []ChangedLineItem     `json:"line_items,omitempty"`
	Subscriptions []ChangedSubscription `json:"subscriptions,omitempty"`
	Invoices      []ChangedInvoice      `json:"invoices,omitempty"`
}

// SubscriptionModifyResponse is the response from execute and preview endpoints.
type SubscriptionModifyResponse struct {
	// The subscription after the modification.
	Subscription *SubscriptionResponse `json:"subscription"`
	// All resources created or mutated as a result of this modification.
	ChangedResources ChangedResources `json:"changed_resources"`
}
