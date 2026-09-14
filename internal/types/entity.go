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
)

// Always present on a create response, so a caller that ignores Status cannot
// mistake a conflict for a success. On FailedAlreadyExists nothing was created
// and EntityId is the pre-existing entity that blocked the request, not the
// caller's; on every other status it is the entity the call produced.
type EntityCreationResult struct {
	Status   EntityCreationStatus `json:"status"`
	EntityId string               `json:"entity_id"`
}

type EntityCreationStatus string

const (
	EntityCreationStatusCreated             EntityCreationStatus = "created"
	EntityCreationStatusSuperseded          EntityCreationStatus = "superseded"
	EntityCreationStatusFailedAlreadyExists EntityCreationStatus = "failed_already_exists"
)

type EntityCreationOptions struct {
	EntityCreationConflictPolicies *EntityCreationConflictPolicies `json:"entity_creation_conflict_policies,omitempty"`
}

type EntityCreationConflictPolicies struct {
	OnExistingEntity OnExistingEntityPolicy `json:"on_existing_entity,omitempty"`
}

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

func (c *EntityCreationConflictPolicies) Validate() error {
	if c == nil {
		return nil
	}
	return c.OnExistingEntity.Validate()
}

func (o *EntityCreationOptions) Validate() error {
	if o == nil {
		return nil
	}
	return o.EntityCreationConflictPolicies.Validate()
}

// A nil options block, nil policies, or an omitted policy all resolve to reject.
func (o *EntityCreationOptions) Policy() OnExistingEntityPolicy {
	if o == nil || o.EntityCreationConflictPolicies == nil || o.EntityCreationConflictPolicies.OnExistingEntity == "" {
		return OnExistingEntityPolicyReject
	}
	return o.EntityCreationConflictPolicies.OnExistingEntity
}
