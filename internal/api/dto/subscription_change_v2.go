package dto

import (
	"time"

	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
)

type SubscriptionChangeV2Request struct {
	TargetPlanID      string                  `json:"target_plan_id" validate:"required"`
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior" validate:"required"`

	EntityPolicies *SubscriptionChangeEntityPolicies `json:"entity_policies,omitempty"`
	IdempotencyKey *string                           `json:"idempotency_key,omitempty" validate:"omitempty"`

	Metadata map[string]string `json:"metadata,omitempty"`
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

type EntityChangeResult struct {
	EntityType  types.SubscriptionLineItemEntityType `json:"entity_type"`
	ReferenceID string                               `json:"reference_id"`
	EntityID    string                               `json:"entity_id"`
	Behaviour   types.EntityChangeBehaviour          `json:"behaviour"`
}

func (r *SubscriptionChangeV2Request) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	if err := r.ProrationBehavior.Validate(); err != nil {
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

	Warnings []string          `json:"warnings,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}
