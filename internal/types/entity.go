package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// SystemEntityType represents the type of entity for system events
type SystemEntityType string

const (
	SystemEntityTypeFeature                SystemEntityType = "feature"
	SystemEntityTypeCustomer               SystemEntityType = "customer"
	SystemEntityTypePlan                   SystemEntityType = "plan"
	SystemEntityTypeSubscription           SystemEntityType = "subscription"
	SystemEntityTypeInvoice                SystemEntityType = "invoice"
	SystemEntityTypePayment                SystemEntityType = "payment"
	SystemEntityTypeCreditNote             SystemEntityType = "credit_note"
	SystemEntityTypeRefund                 SystemEntityType = "refund"
	SystemEntityTypeWallet                 SystemEntityType = "wallet"
	SystemEntityTypeEntitlement            SystemEntityType = "entitlement"
	SystemEntityTypeCheckoutSession        SystemEntityType = "checkout_session"
	SystemEntityTypeEvent                  SystemEntityType = "event"
	SystemEntityTypePrice                  SystemEntityType = "price"
	SystemEntityTypeWalletTransaction      SystemEntityType = "wallet_transaction"
	SystemEntityTypeEntitlementGrant       SystemEntityType = "entitlement_grant"
	SystemEntityTypePaymentMethod          SystemEntityType = "payment_method"
	SystemEntityTypeInvoiceLineItem        SystemEntityType = "invoice_line_item"
	SystemEntityTypeSubscriptionLineItem   SystemEntityType = "subscription_line_item"
	SystemEntityTypeSubscriptionPhase      SystemEntityType = "subscription_phase"
	SystemEntityTypeSubscriptionSchedule   SystemEntityType = "subscription_schedule"
	SystemEntityTypeSubscriptionPause      SystemEntityType = "subscription_pause"
	SystemEntityTypeCreditGrant            SystemEntityType = "credit_grant"
	SystemEntityTypeCreditGrantApplication SystemEntityType = "credit_grant_application"
	SystemEntityTypeCouponAssociation      SystemEntityType = "coupon_association"
	SystemEntityTypeCouponApplication      SystemEntityType = "coupon_application"
	SystemEntityTypeAddonAssociation       SystemEntityType = "addon_association"
	SystemEntityTypeTaxAssociation         SystemEntityType = "tax_association"
)

type EntityCreationStatus string

const (
	EntityCreationStatusCreated             EntityCreationStatus = "created"
	EntityCreationStatusSuperseded          EntityCreationStatus = "superseded"
	EntityCreationStatusFailedAlreadyExists EntityCreationStatus = "failed_already_exists"
)

type OnExistingEntityPolicy string

const (
	OnExistingEntityPolicyReject    OnExistingEntityPolicy = "reject"
	OnExistingEntityPolicySupersede OnExistingEntityPolicy = "supersede"
)

func (s EntityCreationStatus) String() string { return string(s) }

func (p OnExistingEntityPolicy) String() string { return string(p) }

func (p OnExistingEntityPolicy) Validate() error {
	allowed := []OnExistingEntityPolicy{
		OnExistingEntityPolicyReject,
		OnExistingEntityPolicySupersede,
	}
	if p != "" && !lo.Contains(allowed, p) {
		return ierr.NewError("invalid on existing entity policy").
			WithHint("Allowed values: reject, supersede").
			WithReportableDetails(map[string]any{"allowed_values": allowed}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
