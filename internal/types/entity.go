package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// SystemEntityType represents the type of entity for system events
type SystemEntityType string

const (
	SystemEntityTypeFeature         SystemEntityType = "feature"
	SystemEntityTypeCustomer        SystemEntityType = "customer"
	SystemEntityTypePlan            SystemEntityType = "plan"
	SystemEntityTypeSubscription    SystemEntityType = "subscription"
	SystemEntityTypeInvoice         SystemEntityType = "invoice"
	SystemEntityTypePayment         SystemEntityType = "payment"
	SystemEntityTypeCreditNote      SystemEntityType = "credit_note"
	SystemEntityTypeRefund          SystemEntityType = "refund"
	SystemEntityTypeWallet          SystemEntityType = "wallet"
	SystemEntityTypeEntitlement     SystemEntityType = "entitlement"
	SystemEntityTypeCheckoutSession SystemEntityType = "checkout_session"
	SystemEntityTypeEvent           SystemEntityType = "event"
	SystemEntityTypePrice           SystemEntityType = "price"
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
