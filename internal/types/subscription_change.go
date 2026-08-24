package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

type EntityChangeBehaviour string

const (
	EntityChangeBehaviourCarry EntityChangeBehaviour = "carry"
	EntityChangeBehaviourDrop  EntityChangeBehaviour = "drop"
	EntityChangeBehaviourAdd   EntityChangeBehaviour = "add"
)

var EntityChangeBehaviourValues = []EntityChangeBehaviour{
	EntityChangeBehaviourCarry,
	EntityChangeBehaviourDrop,
}

type SubscriptionChangeEntityType string

const (
	SubscriptionChangeEntityTypePlan             SubscriptionChangeEntityType = "plan"
	SubscriptionChangeEntityTypeAddon            SubscriptionChangeEntityType = "addon"
	SubscriptionChangeEntityTypeCreditGrant      SubscriptionChangeEntityType = "credit_grant"
	SubscriptionChangeEntityTypeEntitlement      SubscriptionChangeEntityType = "entitlement"
	SubscriptionChangeEntityTypeEntitlementGrant SubscriptionChangeEntityType = "entitlement_grant"
)

func (t SubscriptionChangeEntityType) String() string { return string(t) }

func (d EntityChangeBehaviour) String() string { return string(d) }

func (d EntityChangeBehaviour) Validate() error {
	if d == "" {
		return nil
	}
	if !lo.Contains(EntityChangeBehaviourValues, d) {
		return ierr.NewError("invalid entity change behaviour").
			WithHint("Behaviour must be one of the allowed values").
			WithReportableDetails(map[string]any{
				"behaviour": string(d),
				"allowed":   EntityChangeBehaviourValues,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

type OnPendingSchedulePolicy string

const (
	OnPendingSchedulePolicyReject    OnPendingSchedulePolicy = "reject"
	OnPendingSchedulePolicySupersede OnPendingSchedulePolicy = "supersede"
)

var OnPendingSchedulePolicyValues = []OnPendingSchedulePolicy{
	OnPendingSchedulePolicyReject,
	OnPendingSchedulePolicySupersede,
}

func (p OnPendingSchedulePolicy) String() string { return string(p) }

func (p OnPendingSchedulePolicy) Validate() error {
	if p == "" {
		return nil
	}
	if !lo.Contains(OnPendingSchedulePolicyValues, p) {
		return ierr.NewError("invalid on_pending_schedule policy").
			WithHint("Policy must be one of the allowed values").
			WithReportableDetails(map[string]any{
				"on_pending_schedule": string(p),
				"allowed":             OnPendingSchedulePolicyValues,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

// BillingPeriodBehaviour decides what a plan change does to the subscription's billing
// anchor and period bounds.
type BillingPeriodBehaviour string

const (
	// BillingPeriodBehaviourUnchanged keeps the anchor and period bounds. Default.
	BillingPeriodBehaviourUnchanged BillingPeriodBehaviour = "unchanged"

	// BillingPeriodBehaviourAnchorAtEffect moves the anchor to the moment the change
	// takes effect and starts a full new period there. For a period-end change that
	// instant is already the anchor, so nothing moves.
	BillingPeriodBehaviourAnchorAtEffect BillingPeriodBehaviour = "anchor_at_effect"

	// BillingPeriodBehaviourAnchorAtConfig moves the anchor to a caller-supplied
	// instant in billing_period_config. Not supported yet: the request validator
	// rejects it.
	BillingPeriodBehaviourAnchorAtConfig BillingPeriodBehaviour = "anchor_at_config"
)

// BillingPeriodBehaviourValues is the parse set, not the accepted set: it admits
// anchor_at_config so a caller sending it gets "not supported yet" rather than
// "invalid billing_period_behaviour".
var BillingPeriodBehaviourValues = []BillingPeriodBehaviour{
	BillingPeriodBehaviourUnchanged,
	BillingPeriodBehaviourAnchorAtEffect,
	BillingPeriodBehaviourAnchorAtConfig,
}

func (b BillingPeriodBehaviour) String() string { return string(b) }

func (b BillingPeriodBehaviour) Validate() error {
	if b == "" {
		return nil
	}
	if !lo.Contains(BillingPeriodBehaviourValues, b) {
		return ierr.NewError("invalid billing_period_behaviour").
			WithHint("Behaviour must be one of the allowed values").
			WithReportableDetails(map[string]any{
				"billing_period_behaviour": string(b),
				"allowed":                  BillingPeriodBehaviourValues,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
