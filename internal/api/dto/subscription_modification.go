package dto

import (
	"strings"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
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

// checkoutAllowedModifyTypes is the allowlist of modification types that accept checkout.
var checkoutAllowedModifyTypes = []SubscriptionModifyType{
	SubscriptionModifyTypeQuantityChange,
	SubscriptionModifyTypeLineItemChange,
	SubscriptionModifyTypeAddon,
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
	Quantity decimal.Decimal `json:"quantity" swaggertype:"string"`
	// EffectiveDate is when the quantity change takes effect.
	// If omitted, the change is effective immediately (now).
	EffectiveDate *time.Time `json:"effective_date,omitempty"`
}

// SubModifyQuantityChangeRequest is the payload for mid-cycle seat/quantity changes.
//
// Deprecated: use SubModifyLineItemChangeRequest with type "line_item_change", which changes
// quantity, price, or both through the shared proration layer.
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
		if li.Quantity.IsNegative() {
			return ierr.NewError("quantity must be non-negative").
				WithHint("Quantity cannot be negative").
				Mark(ierr.ErrValidation)
		}
	}
	return nil
}

// LineItemChange changes a single line item's quantity, price, or both.
type LineItemChange struct {
	ID       string           `json:"id" binding:"required"`
	Quantity *decimal.Decimal `json:"quantity,omitempty" swaggertype:"string"`
	// Amount reprices the charge. Flat fee only for now.
	Amount        *decimal.Decimal `json:"amount,omitempty" swaggertype:"string"`
	EffectiveDate *time.Time       `json:"effective_date,omitempty"`
}

func (c *LineItemChange) ToOverrideLineItemRequest(priceID string) OverrideLineItemRequest {
	return OverrideLineItemRequest{
		PriceID: priceID,
		Amount:  c.Amount,
	}
}

// PriceChange reports whether the change carries a new price configuration.
func (c *LineItemChange) PriceChange() bool {
	return c.Amount != nil
}

type SubModifyLineItemChangeRequest struct {
	LineItems []LineItemChange `json:"line_items" binding:"required,min=1"`
}

func (r *SubModifyLineItemChangeRequest) Validate() error {
	if r == nil || len(r.LineItems) == 0 {
		return ierr.NewError("at least one line item is required").
			WithHint("Provide line_items with at least one entry").
			Mark(ierr.ErrValidation)
	}
	seen := make(map[string]struct{}, len(r.LineItems))

	for i := range r.LineItems {
		li := &r.LineItems[i]
		if li.ID == "" {
			return ierr.NewError("line item ID is required").
				WithHint("Each line_item entry must have a non-empty id").
				Mark(ierr.ErrValidation)
		}
		if _, ok := seen[li.ID]; ok {
			return ierr.NewError("duplicate line item id in line_items").
				WithHint("Each line item can be changed at most once per request; combine quantity and amount into a single entry").
				WithReportableDetails(map[string]any{"line_item_id": li.ID}).
				Mark(ierr.ErrValidation)
		}
		seen[li.ID] = struct{}{}

		if li.Quantity == nil && !li.PriceChange() {
			return ierr.NewError("line item change must set a quantity or an amount").
				WithHint("Provide quantity and/or amount").
				WithReportableDetails(map[string]any{"line_item_id": li.ID}).
				Mark(ierr.ErrValidation)
		}
		if li.Quantity != nil && li.Quantity.IsNegative() {
			return ierr.NewError("quantity must be non-negative").
				WithHint("Quantity cannot be negative").
				WithReportableDetails(map[string]any{"line_item_id": li.ID}).
				Mark(ierr.ErrValidation)
		}
		if li.Amount != nil && li.Amount.IsNegative() {
			return ierr.NewError("amount must be non-negative").
				WithHint("Amount cannot be negative").
				WithReportableDetails(map[string]any{"line_item_id": li.ID}).
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
	SubscriptionModifyTypeAddon            SubscriptionModifyType = "addon"
	SubscriptionModifyTypeLineItemChange   SubscriptionModifyType = "line_item_change"
)

type SubscriptionModificationAction string

const (
	SubscriptionModificationActionAdd    SubscriptionModificationAction = "add"
	SubscriptionModificationActionRemove SubscriptionModificationAction = "remove"
)

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

type SubModifyAddonParams struct {
	Action SubscriptionModificationAction `json:"action" binding:"required"`
	Add    *AddAddonToSubscriptionRequest `json:"add,omitempty"`
	Remove *RemoveAddonRequest            `json:"remove,omitempty"`
}

func (r *SubModifyAddonParams) Validate() error {
	switch r.Action {
	case SubscriptionModificationActionAdd:
		if r.Add == nil {
			return ierr.NewError("add is required for addon action 'add'").
				WithHint("Provide the addon to attach under addon_params.add").
				Mark(ierr.ErrValidation)
		}
		return r.Add.Validate()

	case SubscriptionModificationActionRemove:
		if r.Remove == nil {
			return ierr.NewError("remove is required for addon action 'remove'").
				WithHint("Provide the addon association to end under addon_params.remove").
				Mark(ierr.ErrValidation)
		}
		return r.Remove.Validate()

	default:
		return ierr.NewError("unknown addon action: " + string(r.Action)).
			WithHint("Valid values: add, remove").
			Mark(ierr.ErrValidation)
	}
}

const maxAddonBatchEntries = 20

// SubModifyBulkAddonParams adds and removes several addons as one change, settled as one netted
// document. Entries are the single-addon requests verbatim, so every per-entry rule is shared.
// Reached through type "addon" by sending addon_bulk_params instead of addon_params.
type SubModifyBulkAddonParams struct {
	Adds    []*AddAddonToSubscriptionRequest `json:"adds,omitempty"`
	Removes []*RemoveAddonRequest            `json:"removes,omitempty"`
}

func (p *SubModifyBulkAddonParams) Validate() error {
	if p == nil {
		return ierr.NewError("addon_bulk_params is required").
			Mark(ierr.ErrValidation)
	}

	return ValidateAddonBatch(p.Adds, p.Removes)
}

// ValidateAddonBatch checks a set of addon changes applied as one. Shared with subscription
// creation, which attaches its addons the same way.
func ValidateAddonBatch(adds []*AddAddonToSubscriptionRequest, removes []*RemoveAddonRequest) error {
	total := len(adds) + len(removes)
	if total == 0 {
		return ierr.NewError("at least one add or remove is required").
			WithHint("Provide adds and/or removes with at least one entry").
			Mark(ierr.ErrValidation)
	}
	if total > maxAddonBatchEntries {
		return ierr.NewError("too many addon entries in one request").
			WithHintf("An addon batch accepts at most %d entries across adds and removes", maxAddonBatchEntries).
			WithReportableDetails(map[string]any{"count": total, "max": maxAddonBatchEntries}).
			Mark(ierr.ErrValidation)
	}

	for i, add := range adds {
		if add == nil {
			return ierr.NewError("add entry must not be null").
				WithReportableDetails(map[string]any{"index": i}).
				Mark(ierr.ErrValidation)
		}
		if err := add.Validate(); err != nil {
			return err
		}
		if add.StartDate != nil && add.StartDate.IsZero() {
			return ierr.NewError("start_date must not be zero").
				WithHint("Omit start_date to attach now, or provide a real timestamp").
				WithReportableDetails(map[string]any{"index": i, "addon_id": add.AddonID}).
				Mark(ierr.ErrValidation)
		}
	}

	seen := make(map[string]struct{}, len(removes))
	for i, remove := range removes {
		if remove == nil {
			return ierr.NewError("remove entry must not be null").
				WithReportableDetails(map[string]any{"index": i}).
				Mark(ierr.ErrValidation)
		}
		if err := remove.Validate(); err != nil {
			return err
		}
		if _, ok := seen[remove.AddonAssociationID]; ok {
			return ierr.NewError("duplicate addon_association_id in removes").
				WithHint("Each addon association can be removed at most once per request").
				WithReportableDetails(map[string]any{"index": i, "addon_association_id": remove.AddonAssociationID}).
				Mark(ierr.ErrValidation)
		}
		seen[remove.AddonAssociationID] = struct{}{}

		if remove.EffectiveDate != nil && remove.EffectiveDate.IsZero() {
			return ierr.NewError("effective_date must not be zero").
				WithHint("Omit effective_date to remove at period end, or provide a real timestamp").
				WithReportableDetails(map[string]any{"index": i, "addon_association_id": remove.AddonAssociationID}).
				Mark(ierr.ErrValidation)
		}
	}

	return nil
}

// ExecuteSubscriptionModifyRequest is the unified body for
// POST /subscriptions/:id/modify/execute and /modify/preview.
// Exactly one of the *Params fields must be set, matching the type.
// Optional Checkout opts into pay-first for allowlisted types (e.g. quantity_change).
type ExecuteSubscriptionModifyRequest struct {
	Type                   SubscriptionModifyType           `json:"type" binding:"required"`
	InheritanceParams      *SubModifyInheritanceRequest     `json:"inheritance_params,omitempty"`
	QuantityChangeParams   *SubModifyQuantityChangeRequest  `json:"quantity_change_params,omitempty"`
	LineItemChangeParams   *SubModifyLineItemChangeRequest  `json:"line_item_change_params,omitempty"`
	GroupedInvoicingParams *SubModifyGroupedInvoicingParams `json:"grouped_invoicing_params,omitempty"`
	TrialEndParams         *SubModifyTrialEndRequest        `json:"trial_end_params,omitempty"`
	CouponParams           *SubModifyCouponParams           `json:"coupon_params,omitempty"`
	TaxParams              *SubModifyTaxParams              `json:"tax_params,omitempty"`
	AddonParams            *SubModifyAddonParams            `json:"addon_params,omitempty"`
	BulkAddonParams        *SubModifyBulkAddonParams        `json:"addon_bulk_params,omitempty"`
	Checkout               *CheckoutParams                  `json:"checkout,omitempty"`
}

func (r *ExecuteSubscriptionModifyRequest) Validate() error {
	var err error
	switch r.Type {
	case SubscriptionModifyTypeInheritance:
		if r.InheritanceParams == nil {
			return ierr.NewError("inheritance_params is required for type 'inheritance'").
				Mark(ierr.ErrValidation)
		}
		err = r.InheritanceParams.Validate()
	case SubscriptionModifyTypeQuantityChange:
		if r.QuantityChangeParams == nil {
			return ierr.NewError("quantity_change_params is required for type 'quantity_change'").
				Mark(ierr.ErrValidation)
		}
		err = r.QuantityChangeParams.Validate()
	case SubscriptionModifyTypeLineItemChange:
		if r.LineItemChangeParams == nil {
			return ierr.NewError("line_item_change_params is required for type 'line_item_change'").
				Mark(ierr.ErrValidation)
		}
		err = r.LineItemChangeParams.Validate()
	case SubscriptionModifyTypeGroupedInvoicing:
		if r.GroupedInvoicingParams == nil {
			return ierr.NewError("grouped_invoicing_params is required for type 'grouped_invoicing'").
				Mark(ierr.ErrValidation)
		}
		err = r.GroupedInvoicingParams.Validate()
	case SubscriptionModifyTypeTrialEnd:
		if r.TrialEndParams == nil {
			return ierr.NewError("trial_end_params is required for type 'trial_end'").
				Mark(ierr.ErrValidation)
		}
		err = r.TrialEndParams.Validate()
	case SubscriptionModifyTypeCoupon:
		if r.CouponParams == nil {
			return ierr.NewError("coupon_params is required for type 'coupon'").
				Mark(ierr.ErrValidation)
		}
		err = r.CouponParams.Validate()
	case SubscriptionModifyTypeTax:
		if r.TaxParams == nil {
			return ierr.NewError("tax_params is required for type 'tax'").
				Mark(ierr.ErrValidation)
		}
		err = r.TaxParams.Validate()
	case SubscriptionModifyTypeAddon:
		// One addon or several: addon_params changes a single addon, addon_bulk_params
		// changes a set of them as one netted document. Exactly one of the two.
		switch {
		case r.AddonParams != nil && r.BulkAddonParams != nil:
			return ierr.NewError("addon_params and addon_bulk_params are mutually exclusive").
				WithHint("Send addon_params to change one addon, or addon_bulk_params to change several").
				Mark(ierr.ErrValidation)
		case r.BulkAddonParams != nil:
			err = r.BulkAddonParams.Validate()
		case r.AddonParams != nil:
			err = r.AddonParams.Validate()
		default:
			return ierr.NewError("addon_params or addon_bulk_params is required for type 'addon'").
				WithHint("Provide addon_params with an action of add or remove, " +
					"or addon_bulk_params with at least one add or remove").
				Mark(ierr.ErrValidation)
		}
	default:
		return ierr.NewError("unknown modification type: " + string(r.Type)).
			WithHint("Valid values: inheritance, quantity_change, line_item_change, grouped_invoicing, trial_end, coupon, tax, addon").
			Mark(ierr.ErrValidation)
	}
	if err != nil {
		return err
	}
	return r.validateCheckout()
}

func (r *ExecuteSubscriptionModifyRequest) validateCheckout() error {
	if r.Checkout == nil {
		return nil
	}
	if !lo.Contains(checkoutAllowedModifyTypes, r.Type) {
		return ierr.NewError("checkout is not supported for this modification type").
			WithHintf("checkout is only allowed for %v", checkoutAllowedModifyTypes).
			WithReportableDetails(map[string]any{
				"type":          r.Type,
				"allowed_types": checkoutAllowedModifyTypes,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.Type == SubscriptionModifyTypeAddon && r.AddonParams != nil &&
		r.AddonParams.Action == SubscriptionModificationActionRemove {
		return ierr.NewError("checkout is not supported when removing an addon").
			WithHint("Removing an addon issues a credit, so there is no payment to collect").
			Mark(ierr.ErrValidation)
	}

	return r.Checkout.Validate()
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

// ChangedAddonAssociationAction describes how an addon association changed.
// @Description created | ended
type ChangedAddonAssociationAction string

const (
	ChangedAddonAssociationActionCreated ChangedAddonAssociationAction = "created"
	ChangedAddonAssociationActionEnded   ChangedAddonAssociationAction = "ended"
)

// ChangedInvoiceAction classifies invoice-side effects from a modification.
// @Description created (proration invoice) | wallet_credit (downgrade credit)
type ChangedInvoiceAction string

const (
	ChangedInvoiceActionCreated      ChangedInvoiceAction = "created"
	ChangedInvoiceActionWalletCredit ChangedInvoiceAction = "wallet_credit"
)

// ChangedInvoiceStatus is the high-level status for ChangedInvoice.
// "preview" and "issued" are used for dry-run and wallet credits.
// Persisted invoices use the same strings as types.PaymentStatus.
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
}

// ChangedAddonAssociation describes an addon attached or ended by a modification. It is the
// only place an attach's new association id is returned, and a later removal needs it.
type ChangedAddonAssociation struct {
	ID           string                        `json:"id"`
	AddonID      string                        `json:"addon_id"`
	AddonStatus  types.AddonStatus             `json:"addon_status"`
	StartDate    *time.Time                    `json:"start_date,omitempty"`
	EndDate      *time.Time                    `json:"end_date,omitempty"`
	ChangeAction ChangedAddonAssociationAction `json:"change_action" enums:"created,ended"`
}

// ChangedInvoice describes a proration invoice or wallet credit from a modification.
type ChangedInvoice struct {
	ID string `json:"id"`
	// Action is created for a proration charge invoice, wallet_credit for downgrade credit.
	Action ChangedInvoiceAction `json:"action"`
	// Status is preview (dry-run), issued (wallet credit applied), or a PaymentStatus string for real invoices.
	Status ChangedInvoiceStatus `json:"status" enums:"preview,issued,INITIATED,PENDING,PROCESSING,SUCCEEDED,OVERPAID,FAILED,REFUNDED,PARTIALLY_REFUNDED,VOIDED"`
	// Invoice is set for proration charges: preview returns a synthetic invoice; execute returns the persisted invoice when created.
	Invoice *InvoiceResponse `json:"invoice,omitempty"`
	// WalletTransaction is set for downgrade wallet credits: preview is synthetic; execute returns the transaction from the top-up.
	WalletTransaction *WalletTransactionResponse `json:"wallet_transaction,omitempty"`
}

// ChangedResources is the Orb-inspired envelope for all mutation side-effects.
type ChangedResources struct {
	LineItems         []ChangedLineItem         `json:"line_items,omitempty"`
	AddonAssociations []ChangedAddonAssociation `json:"addon_associations,omitempty"`
	Subscriptions     []ChangedSubscription     `json:"subscriptions,omitempty"`
	Invoices          []ChangedInvoice          `json:"invoices,omitempty"`
}

// SubscriptionModifyResponse is the response from execute and preview endpoints.
type SubscriptionModifyResponse struct {
	// The subscription after the modification.
	Subscription *SubscriptionResponse `json:"subscription"`
	// All resources created or mutated as a result of this modification.
	ChangedResources ChangedResources `json:"changed_resources"`
	// CheckoutSession is set on pay-first execute when payment must be collected
	// before line-item changes apply. Reuses CheckoutSessionResponse (payment_action
	// at session root). Omitted for pay-later and preview.
	CheckoutSession *CheckoutSessionResponse `json:"checkout_session,omitempty"`
}
