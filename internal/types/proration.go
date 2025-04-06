package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// ProrationAction defines the type of proration action being performed
type ProrationAction string

const (
	ProrationActionPlanChange     ProrationAction = "plan_change"
	ProrationActionQuantityChange ProrationAction = "quantity_change"
	ProrationActionCancellation   ProrationAction = "cancellation"
	ProrationActionAddItem        ProrationAction = "add_item"
	ProrationActionRemoveItem     ProrationAction = "remove_item"
)

func (a ProrationAction) Validate() error {
	allowedActions := []ProrationAction{
		ProrationActionPlanChange,
		ProrationActionQuantityChange,
		ProrationActionCancellation,
		ProrationActionAddItem,
		ProrationActionRemoveItem,
	}

	if !lo.Contains(allowedActions, a) {
		return ierr.NewError("invalid proration action").
			WithHint("Proration action is not valid").
			WithReportableDetails(map[string]any{
				"allowed_actions": allowedActions,
				"action":          a,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
	return nil
}

// ProrationBehavior defines how prorations should be handled i.e
// should we create prorated line items or immediately invoice the prorated amount
// or not perform any prorations at all
type ProrationBehavior string

const (
	// CreateProrations will create prorated line items (default)
	ProrationBehaviorCreateProrations ProrationBehavior = "create_prorations"

	// AlwaysInvoice will immediately invoice prorated amounts
	ProrationBehaviorAlwaysInvoice ProrationBehavior = "always_invoice"

	// None will not perform any prorations
	ProrationBehaviorNone ProrationBehavior = "none"
)

func (b ProrationBehavior) Validate() error {
	allowedBehaviors := []ProrationBehavior{
		ProrationBehaviorCreateProrations,
		ProrationBehaviorAlwaysInvoice,
		ProrationBehaviorNone,
	}

	if !lo.Contains(allowedBehaviors, b) {
		return ierr.NewError("invalid proration behavior").
			WithHint("Proration behavior is not valid").
			WithReportableDetails(map[string]any{
				"allowed_behaviors": allowedBehaviors,
				"behavior":          b,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
	return nil
}

// ProrationStrategy defines how proration coefficients are calculated
type ProrationStrategy string

const (
	// DayBased calculates proration based on days (default)
	ProrationStrategyDayBased ProrationStrategy = "day_based"

	// SecondBased calculates proration based on seconds
	ProrationStrategySecondBased ProrationStrategy = "second_based"
)

func (s ProrationStrategy) Validate() error {
	allowedStrategies := []ProrationStrategy{
		ProrationStrategyDayBased,
		ProrationStrategySecondBased,
	}

	if !lo.Contains(allowedStrategies, s) {
		return ierr.NewError("invalid proration strategy").
			WithHint("Proration strategy is not valid").
			WithReportableDetails(map[string]any{
				"allowed_strategies": allowedStrategies,
				"strategy":           s,
			}).
			Mark(ierr.ErrInvalidOperation)
	}
	return nil
}

type PlanChangeType string

const (
	PlanChangeTypeUpgrade   PlanChangeType = "upgrade"
	PlanChangeTypeDowngrade PlanChangeType = "downgrade"
	PlanChangeTypeNoChange  PlanChangeType = "no_change"
)
