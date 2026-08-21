package dto

import (
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

	Metadata map[string]string `json:"metadata,omitempty"`
}

func (r *SubscriptionChangeV2Request) IsDeferred() bool {
	return r != nil && r.ChangeAt != nil && *r.ChangeAt == types.ScheduleTypePeriodEnd
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

func (p *EntityChangePolicy) BehaviourFor(referenceID string) types.EntityChangeBehaviour {
	if p == nil {
		return types.EntityChangeBehaviourCarry
	}
	if d, ok := p.Overrides[referenceID]; ok && d != "" {
		return d
	}
	if p.DefaultBehaviour != "" {
		return p.DefaultBehaviour
	}
	return types.EntityChangeBehaviourCarry
}

// EntityChangeResult reports one entity the plan change carried, dropped or added.
// ReferenceID identifies the affected record (addon association, credit grant,
// entitlement); EntityID identifies what it came from (addon, plan).
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

	if r.EntityPolicies != nil {
		return r.EntityPolicies.Addons.Validate()
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
	SupersededSchedules []string `json:"superseded_schedules,omitempty"`

	Warnings []string          `json:"warnings,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
