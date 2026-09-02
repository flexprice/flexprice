package dto

import (
	"fmt"
	"time"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type SubscriptionChangeV2Request struct {
	TargetPlanID      string                  `json:"target_plan_id" validate:"required"`
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior" validate:"required"`

	EntityPolicies *SubscriptionChangeEntityPolicies `json:"entity_policies,omitempty"`
	IdempotencyKey *string                           `json:"idempotency_key,omitempty" validate:"omitempty"`

	// ChangeAt controls when the change takes effect. nil or "immediate" applies
	// it now; "end_of_period" schedules it at the subscription's current period end.
	ChangeAt *types.ScheduleType `json:"change_at,omitempty"`

	OnConflictPolicies *SubscriptionChangeConflictPolicies `json:"on_conflict_policies,omitempty"`

	// BillingPeriodBehaviour controls the billing anchor. Omitted means "unchanged":
	// the swap is priced as a prorated delta inside the running period.
	BillingPeriodBehaviour types.BillingPeriodBehaviour `json:"billing_period_behaviour,omitempty"`
	BillingPeriodConfig    *BillingPeriodConfig         `json:"billing_period_config,omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`
}

func (r *SubscriptionChangeV2Request) IsDeferred() bool {
	return r != nil && r.ChangeAt != nil && *r.ChangeAt == types.ScheduleTypePeriodEnd
}

// BillingPeriodConfig defines the billing period for any recurring entity.
type BillingPeriodConfig struct {
	BillingCycle       *types.BillingCycle  `json:"billing_cycle,omitempty"`
	BillingPeriodUnit  *types.BillingPeriod `json:"billing_period_unit,omitempty"`
	BillingPeriodCount *int                 `json:"billing_period_count,omitempty"`
	BillingAnchor      *time.Time           `json:"billing_anchor,omitempty"`
}

// EffectiveBillingPeriodBehaviour resolves the omitted case, so the response never echoes
// an empty behaviour back at the caller.
func (r *SubscriptionChangeV2Request) EffectiveBillingPeriodBehaviour() types.BillingPeriodBehaviour {
	if r == nil || r.BillingPeriodBehaviour == "" {
		return types.BillingPeriodBehaviourUnchanged
	}

	return r.BillingPeriodBehaviour
}

// AnchorAtEffect reports whether the change restarts the term at the instant it
// takes effect.
func (r *SubscriptionChangeV2Request) AnchorAtEffect() bool {
	return r != nil && r.BillingPeriodBehaviour == types.BillingPeriodBehaviourAnchorAtEffect
}

// SubscriptionChangeConflictPolicies decides how the change reacts to state that
// would otherwise block it.
type SubscriptionChangeConflictPolicies struct {
	// OnPendingSchedule applies to a queued plan change only. A pending cancellation
	// or pause is still rejected outright: clearing those would mean un-cancelling or
	// auto-resuming the subscription.
	OnPendingSchedule types.OnPendingSchedulePolicy `json:"on_pending_schedule,omitempty"`
}

func (p *SubscriptionChangeConflictPolicies) Validate() error {
	if p == nil {
		return nil
	}
	return p.OnPendingSchedule.Validate()
}

// SupersedesPendingSchedule reports whether a queued plan change should be cancelled
// in favour of this request.
func (r *SubscriptionChangeV2Request) SupersedesPendingSchedule() bool {
	return r != nil && r.OnConflictPolicies != nil &&
		r.OnConflictPolicies.OnPendingSchedule == types.OnPendingSchedulePolicySupersede
}

type SubscriptionChangeEntityPolicies struct {
	Addons *EntityChangePolicy `json:"addons,omitempty"`
}

type EntityChangePolicy struct {
	DefaultBehaviour types.EntityChangeBehaviour `json:"default_behaviour,omitempty"`

	// Overrides is keyed by addon_associations.id (instance), not catalogue addon_id.
	// That is the id an EntityChangeResult reports as EntityID.
	Overrides map[string]types.EntityChangeBehaviour `json:"overrides,omitempty"`
}

func (p *EntityChangePolicy) Validate() error {
	if p == nil {
		return nil
	}

	if err := p.DefaultBehaviour.Validate(); err != nil {
		return err
	}

	for _, behaviour := range p.Overrides {
		if err := behaviour.Validate(); err != nil {
			return err
		}
	}
	return nil
}

func (p *EntityChangePolicy) BehaviourFor(entityID string) types.EntityChangeBehaviour {
	if p == nil {
		return types.EntityChangeBehaviourCarry
	}
	if d, ok := p.Overrides[entityID]; ok && d != "" {
		return d
	}
	if p.DefaultBehaviour != "" {
		return p.DefaultBehaviour
	}
	return types.EntityChangeBehaviourCarry
}

// EntityChangeResult is one entity the change touched. EntityID identifies the entity
// itself and is the id an addon override is keyed by; ReferenceID is what that entity
// refers to — the catalogue addon, the plan a credit grant belongs to, the feature an
// entitlement covers.
type EntityChangeResult struct {
	EntityType  types.SubscriptionChangeEntityType `json:"entity_type"`
	ReferenceID string                             `json:"reference_id"`
	EntityID    string                             `json:"entity_id"`
	Behaviour   types.EntityChangeBehaviour        `json:"behaviour"`
}

func (r *SubscriptionChangeV2Request) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.ProrationBehavior.Validate(); err != nil {
		return err
	}

	if r.ChangeAt != nil {
		if err := r.ChangeAt.Validate(); err != nil {
			return err
		}
	}

	if r.IsDeferred() && r.ProrationBehavior == types.ProrationBehaviorCreateProrations {
		return ierr.NewError("proration is not applicable for an end_of_period plan change").
			WithHint("Set proration_behavior to 'none'. At a period boundary the renewal invoice already bills the new plan.").
			Mark(ierr.ErrValidation)
	}

	if err := r.OnConflictPolicies.Validate(); err != nil {
		return err
	}

	if err := r.validateBillingPeriodBehaviour(); err != nil {
		return err
	}

	if r.EntityPolicies != nil {
		return r.EntityPolicies.Addons.Validate()
	}
	return nil
}

// SubscriptionChangeBillingPeriodResult is the anchor state after the change. For a
// deferred change it is the state at request time: the boundary has not arrived yet.
type SubscriptionChangeBillingPeriodResult struct {
	Behaviour          types.BillingPeriodBehaviour `json:"behaviour"`
	BillingAnchor      time.Time                    `json:"billing_anchor"`
	CurrentPeriodStart time.Time                    `json:"current_period_start"`
	CurrentPeriodEnd   time.Time                    `json:"current_period_end"`
}

// validateBillingPeriodBehaviour enforces the behaviour matrix. reset_at is rejected
// outright rather than inferred as reset_at_effect: the enum already has a value that
// says "reset at effective", so inferring one from the other would only hide a mistake.
func (r *SubscriptionChangeV2Request) validateBillingPeriodBehaviour() error {
	if err := r.BillingPeriodBehaviour.Validate(); err != nil {
		return err
	}

	if r.BillingPeriodConfig != nil {
		return ierr.NewError("billing_period_config is not supported yet").
			WithHint(fmt.Sprintf(
				"Remove billing_period_config. Use billing_period_behaviour='%s' to restart the term at the change instant.",
				types.BillingPeriodBehaviourAnchorAtEffect,
			)).
			Mark(ierr.ErrValidation)
	}

	if r.BillingPeriodBehaviour == types.BillingPeriodBehaviourAnchorAtConfig {
		return ierr.NewError(fmt.Sprintf(
			"billing_period_behaviour '%s' is not supported yet",
			types.BillingPeriodBehaviourAnchorAtConfig,
		)).
			WithHint(fmt.Sprintf(
				"Use '%s' to restart the term at the change instant.",
				types.BillingPeriodBehaviourAnchorAtEffect,
			)).
			WithReportableDetails(map[string]any{
				"supported": []types.BillingPeriodBehaviour{
					types.BillingPeriodBehaviourUnchanged,
					types.BillingPeriodBehaviourAnchorAtEffect,
				},
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

type SubscriptionChangeV2Response struct {
	Subscription     *SubscriptionResponse `json:"subscription"`
	ChangedResources ChangedResources      `json:"changed_resources"`

	ChangeType  types.SubscriptionChangeType `json:"change_type"`
	EffectiveAt time.Time                    `json:"effective_at"`
	FromPlan    PlanSummary                  `json:"from_plan"`
	ToPlan      PlanSummary                  `json:"to_plan"`

	EntityChanges []*EntityChangeResult `json:"entity_changes,omitempty"`

	// IsScheduled is true when the change was deferred to the period end instead
	// of being applied immediately.
	IsScheduled bool       `json:"is_scheduled"`
	ScheduleID  *string    `json:"schedule_id,omitempty"`
	ScheduledAt *time.Time `json:"scheduled_at,omitempty"`

	// SupersededSchedules lists the plan-change schedules this request cancelled under
	// on_conflict_policies.on_pending_schedule. Preview reports what execute would cancel.
	SupersededSchedules []string                              `json:"superseded_schedules,omitempty"`
	BillingPeriod       SubscriptionChangeBillingPeriodResult `json:"billing_period"`

	Warnings []string          `json:"warnings,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
