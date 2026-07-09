package dto

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InheritanceAction identifies whether children are being added to or removed from inheritance.
type InheritanceAction string

const (
	// InheritanceActionAdd adds inherited child subscriptions to a parent.
	InheritanceActionAdd InheritanceAction = "add"
	// InheritanceActionRemove schedules inherited child subscriptions for cancellation at period end.
	InheritanceActionRemove InheritanceAction = "remove"
)

// SubModifyInheritanceRequest is the payload for adding or removing
// inherited child subscriptions from a parent subscription.
type SubModifyInheritanceRequest struct {
	// Action is "add" or "remove". Defaults to "add" when omitted — fully backward-compatible.
	Action InheritanceAction `json:"action,omitempty"`

	// ExternalCustomerIDsToInheritSubscription is used for action="add".
	ExternalCustomerIDsToInheritSubscription []string `json:"external_customer_ids_to_inherit_subscription,omitempty"`

	// ExternalCustomerIDsToRemove is used for action="remove".
	ExternalCustomerIDsToRemove []string `json:"external_customer_ids_to_remove,omitempty"`
}

func (r *SubModifyInheritanceRequest) Validate() error {
	switch r.Action {
	case InheritanceActionRemove:
		if len(r.ExternalCustomerIDsToRemove) == 0 {
			return ierr.NewError("at least one external customer ID is required for remove").
				WithHint("Provide external_customer_ids_to_remove with at least one non-empty value").
				Mark(ierr.ErrValidation)
		}
		for _, id := range r.ExternalCustomerIDsToRemove {
			if strings.TrimSpace(id) == "" {
				return ierr.NewError("external customer ID must not be empty").
					WithHint("Remove any empty strings from external_customer_ids_to_remove").
					Mark(ierr.ErrValidation)
			}
		}
	default: // "" or "add"
		if len(r.ExternalCustomerIDsToInheritSubscription) == 0 {
			return ierr.NewError("at least one external customer ID is required").
				WithHint("Provide external_customer_ids_to_inherit_subscription with at least one non-empty value").
				Mark(ierr.ErrValidation)
		}
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

// TrialEndAction specifies how to modify the trial period.
type TrialEndAction string

const (
	// TrialEndActionImmediate ends the trial immediately and begins conversion.
	TrialEndActionImmediate TrialEndAction = "immediate"
	// TrialEndActionScheduledDate changes the trial end date to a new value.
	TrialEndActionScheduledDate TrialEndAction = "scheduled_date"
)

// SubModifyTrialEndRequest is the payload for modifying a subscription's trial period end.
type SubModifyTrialEndRequest struct {
	// Action is "immediate" or "scheduled_date".
	Action TrialEndAction `json:"action" binding:"required"`
	// NewTrialEnd is the new trial end date. Required when action is "scheduled_date".
	NewTrialEnd *time.Time `json:"new_trial_end,omitempty"`
}

func (r *SubModifyTrialEndRequest) Validate() error {
	switch r.Action {
	case TrialEndActionImmediate:
		// no extra fields needed
	case TrialEndActionScheduledDate:
		if r.NewTrialEnd == nil {
			return ierr.NewError("new_trial_end is required when action is 'scheduled_date'").
				WithHint("Provide a new_trial_end date to extend or reduce the trial").
				Mark(ierr.ErrValidation)
		}
	default:
		return ierr.NewError("unknown trial end action: " + string(r.Action)).
			WithHint("Valid values: immediate, scheduled_date").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SubscriptionModifyType identifies the kind of modification.
type SubscriptionModifyType string

const (
	SubscriptionModifyTypeInheritance      SubscriptionModifyType = "inheritance"
	SubscriptionModifyTypeQuantityChange   SubscriptionModifyType = "quantity_change"
	SubscriptionModifyTypeGroupedInvoicing SubscriptionModifyType = "grouped_invoicing"
	SubscriptionModifyTypeTrialEnd         SubscriptionModifyType = "trial_end"
	SubscriptionModifyTypeCoupon           SubscriptionModifyType = "coupon"
	SubscriptionModifyTypeTax              SubscriptionModifyType = "tax"
	SubscriptionModifyTypeEndDate          SubscriptionModifyType = "end_date"
)

// SubModifyEndDateRequest is the payload for extending a subscription's end date.
type SubModifyEndDateRequest struct {
	// NewEndDate is the new subscription end. Must be strictly after the current end_date.
	NewEndDate time.Time `json:"new_end_date" binding:"required"`
}

func (r *SubModifyEndDateRequest) Validate() error {
	if r.NewEndDate.IsZero() {
		return ierr.NewError("new_end_date is required").
			WithHint("Provide a new_end_date to extend the subscription term").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SubModifyCouponAction is the action to perform on a coupon association.
type SubModifyCouponAction string

const (
	SubModifyCouponActionAdd    SubModifyCouponAction = "add"
	SubModifyCouponActionRemove SubModifyCouponAction = "remove"
)

// SubModifyTaxAction is the action to perform on a tax association.
type SubModifyTaxAction string

const (
	SubModifyTaxActionAdd    SubModifyTaxAction = "add"
	SubModifyTaxActionRemove SubModifyTaxAction = "remove"
)

// GroupedInvoicingAction identifies whether children are being added to or removed from grouped invoicing.
type GroupedInvoicingAction string

const (
	GroupedInvoicingActionAdd    GroupedInvoicingAction = "add"
	GroupedInvoicingActionRemove GroupedInvoicingAction = "remove"
)

// SubModifyGroupedInvoicingParams is the payload for grouped invoicing membership changes.
type SubModifyGroupedInvoicingParams struct {
	// Action specifies whether to add or remove the child subscriptions from grouped invoicing.
	Action GroupedInvoicingAction `json:"action" binding:"required"`
	// ParentSubscriptionID is required for action 'add'.
	ParentSubscriptionID string   `json:"parent_subscription_id,omitempty"`
	ChildSubscriptionIDs []string `json:"child_subscription_ids"`
}

func (r *SubModifyGroupedInvoicingParams) Validate() error {
	if r.Action != GroupedInvoicingActionAdd && r.Action != GroupedInvoicingActionRemove {
		return ierr.NewError("action must be 'add' or 'remove'").
			WithHint("Valid values for grouped_invoicing action: add, remove").
			Mark(ierr.ErrValidation)
	}
	if len(r.ChildSubscriptionIDs) == 0 {
		return ierr.NewError("child_subscription_ids must not be empty").
			WithHint("Provide child_subscription_ids with at least one entry").
			Mark(ierr.ErrValidation)
	}
	if r.Action == GroupedInvoicingActionAdd && r.ParentSubscriptionID == "" {
		return ierr.NewError("parent_subscription_id is required for grouped invoicing action 'add'").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SubModifyCouponParams is the payload for coupon association changes on a subscription.
// For action="add": coupon_code is required; provide either subscription_id (sub-level) or
// subscription_line_item_id (line-item level), but not both.
// For action="remove": association_id is required.
type SubModifyCouponParams struct {
	// Required. "add" to attach a coupon; "remove" to detach an existing association.
	Action SubModifyCouponAction `json:"action" binding:"required"`
	// Required for action="add". Coupon code of the coupon to attach.
	CouponCode *string `json:"coupon_code,omitempty"`
	// Required when action="remove". ID of the CouponAssociation to soft-delete.
	CouponAssociationID *string `json:"coupon_association_id,omitempty"`
	// Optional. When the coupon association starts; defaults to now.
	StartDate *time.Time `json:"start_date,omitempty"`
	// Optional. When the coupon association ends.
	EndDate *time.Time `json:"end_date,omitempty"`
	// Optional. Apply at subscription level. Mutually exclusive with SubscriptionLineItemID.
	SubscriptionID *string `json:"subscription_id,omitempty"`
	// Optional. Apply at a specific line item. Mutually exclusive with SubscriptionID.
	SubscriptionLineItemID *string `json:"subscription_line_item_id,omitempty"`
}

func (r *SubModifyCouponParams) Validate() error {
	switch r.Action {
	case SubModifyCouponActionAdd:
		if r.CouponCode == nil || *r.CouponCode == "" {
			return ierr.NewError("coupon_code is required for action 'add'").
				WithHint("Provide a valid coupon_code").
				Mark(ierr.ErrValidation)
		}
		if r.SubscriptionID != nil && r.SubscriptionLineItemID != nil {
			return ierr.NewError("subscription_id and subscription_line_item_id are mutually exclusive").
				WithHint("Provide at most one of subscription_id or subscription_line_item_id").
				Mark(ierr.ErrValidation)
		}
	case SubModifyCouponActionRemove:
		if r.CouponAssociationID == nil || *r.CouponAssociationID == "" {
			return ierr.NewError("coupon_association_id is required for action 'remove'").
				WithHint("Provide the coupon association ID to remove").
				Mark(ierr.ErrValidation)
		}
	default:
		return ierr.NewError("unknown coupon action: " + string(r.Action)).
			WithHint("Valid values: add, remove").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// SubModifyTaxParams is the payload for tax association changes on a subscription.
// Conditional required fields: tax_rate_id is required when action="add"; association_id is required when action="remove".
type SubModifyTaxParams struct {
	// Required. "add" to attach a tax rate; "remove" to detach an existing association.
	Action SubModifyTaxAction `json:"action" binding:"required"`
	// Required when action="add". ID of the active tax rate to attach.
	TaxRateID *string `json:"tax_rate_id,omitempty"`
	// Required when action="remove". ID of the TaxAssociation to soft-delete.
	TaxAssociationID *string `json:"tax_association_id,omitempty"`
	// Optional. When to apply the change; defaults to now if omitted.
	EffectiveDate *time.Time `json:"effective_date,omitempty"`
}

func (r *SubModifyTaxParams) Validate() error {
	switch r.Action {
	case SubModifyTaxActionAdd:
		if r.TaxRateID == nil || *r.TaxRateID == "" {
			return ierr.NewError("tax_rate_id is required for action 'add'").
				WithHint("Provide a valid tax_rate_id").
				Mark(ierr.ErrValidation)
		}
	case SubModifyTaxActionRemove:
		if r.TaxAssociationID == nil || *r.TaxAssociationID == "" {
			return ierr.NewError("tax_association_id is required for action 'remove'").
				WithHint("Provide the tax association ID to remove").
				Mark(ierr.ErrValidation)
		}
	default:
		return ierr.NewError("unknown tax action: " + string(r.Action)).
			WithHint("Valid values: add, remove").
			Mark(ierr.ErrValidation)
	}
	return nil
}

// ExecuteSubscriptionModifyRequest is the unified body for
// POST /subscriptions/:id/modify/execute and /modify/preview.
// Exactly one of the *Params fields must be set, matching the type.
type ExecuteSubscriptionModifyRequest struct {
	Type                   SubscriptionModifyType           `json:"type" binding:"required"`
	InheritanceParams      *SubModifyInheritanceRequest     `json:"inheritance_params,omitempty"`
	QuantityChangeParams   *SubModifyQuantityChangeRequest  `json:"quantity_change_params,omitempty"`
	GroupedInvoicingParams *SubModifyGroupedInvoicingParams `json:"grouped_invoicing_params,omitempty"`
	TrialEndParams         *SubModifyTrialEndRequest        `json:"trial_end_params,omitempty"`
	CouponParams           *SubModifyCouponParams           `json:"coupon_params,omitempty"`
	TaxParams              *SubModifyTaxParams              `json:"tax_params,omitempty"`
	EndDateParams          *SubModifyEndDateRequest         `json:"end_date_params,omitempty"`
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
	case SubscriptionModifyTypeGroupedInvoicing:
		if r.GroupedInvoicingParams == nil {
			return ierr.NewError("grouped_invoicing_params is required for type 'grouped_invoicing'").
				Mark(ierr.ErrValidation)
		}
		return r.GroupedInvoicingParams.Validate()
	case SubscriptionModifyTypeTrialEnd:
		if r.TrialEndParams == nil {
			return ierr.NewError("trial_end_params is required for type 'trial_end'").
				Mark(ierr.ErrValidation)
		}
		return r.TrialEndParams.Validate()
	case SubscriptionModifyTypeCoupon:
		if r.CouponParams == nil {
			return ierr.NewError("coupon_params is required for type 'coupon'").
				Mark(ierr.ErrValidation)
		}
		return r.CouponParams.Validate()
	case SubscriptionModifyTypeTax:
		if r.TaxParams == nil {
			return ierr.NewError("tax_params is required for type 'tax'").
				Mark(ierr.ErrValidation)
		}
		return r.TaxParams.Validate()
	case SubscriptionModifyTypeEndDate:
		if r.EndDateParams == nil {
			return ierr.NewError("end_date_params is required for type 'end_date'").
				Mark(ierr.ErrValidation)
		}
		return r.EndDateParams.Validate()
	default:
		return ierr.NewError("unknown modification type: " + string(r.Type)).
			WithHint("Valid values: inheritance, quantity_change, grouped_invoicing, trial_end, coupon, tax, end_date").
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
	ID               string                    `json:"id"`
	Action           ChangedSubscriptionAction `json:"action"`
	Status           types.SubscriptionStatus  `json:"status"`
	TrialEnd         *time.Time                `json:"trial_end,omitempty"`
	CurrentPeriodEnd *time.Time                `json:"current_period_end,omitempty"`
	EndDate          *time.Time                `json:"end_date,omitempty"`
}

// ChangedCreditGrantAction describes how a credit grant changed.
// @Description updated
type ChangedCreditGrantAction string

const (
	ChangedCreditGrantActionUpdated ChangedCreditGrantAction = "updated"
)

// ChangedCreditGrant describes a credit grant mutated by a subscription modification.
type ChangedCreditGrant struct {
	ID      string                   `json:"id"`
	Action  ChangedCreditGrantAction `json:"action"`
	EndDate *time.Time               `json:"end_date,omitempty"`
	// CreditGrant is the grant after the modification (execute) or projected (preview).
	CreditGrant *CreditGrantResponse `json:"credit_grant,omitempty"`
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
	CreditGrants  []ChangedCreditGrant  `json:"credit_grants,omitempty"`
}

// SubscriptionModifyResponse is the response from execute and preview endpoints.
type SubscriptionModifyResponse struct {
	// The subscription after the modification.
	Subscription *SubscriptionResponse `json:"subscription"`
	// All resources created or mutated as a result of this modification.
	ChangedResources ChangedResources `json:"changed_resources"`
}
