package types

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
)

// ProrationAction defines the type of change triggering proration.
type ProrationAction string

const (
	ProrationActionUpgrade        ProrationAction = "upgrade"
	ProrationActionDowngrade      ProrationAction = "downgrade"
	ProrationActionQuantityChange ProrationAction = "quantity_change"
	ProrationActionCancellation   ProrationAction = "cancellation"
	ProrationActionAddItem        ProrationAction = "add_item"
	ProrationActionRemoveItem     ProrationAction = "remove_item"
)

// ProrationStrategy defines how the proration coefficient is calculated.
type ProrationStrategy string

const (
	StrategyDayBased    ProrationStrategy = "day_based"    // Default
	StrategySecondBased ProrationStrategy = "second_based" // Future enhancement
)

// ProrationBehavior defines how proration is applied (e.g., create invoice items).
type ProrationBehavior string

const (
	ProrationBehaviorCreateProrations ProrationBehavior = "create_prorations" // Default: Create credits/charges on invoice
	ProrationBehaviorNone             ProrationBehavior = "none"              // Calculate but don't apply (e.g., for previews)
)

// BillingMode represents when a subscription is billed.
type BillingMode string

const (
	BillingModeInAdvance BillingMode = "in_advance"
	BillingModeInArrears BillingMode = "in_arrears"
)

// ScheduleType determines when subscription changes take effect.
type ScheduleType string

const (
	ScheduleTypeImmediate    ScheduleType = "immediate"
	ScheduleTypePeriodEnd    ScheduleType = "period_end"
	ScheduleTypeSpecificDate ScheduleType = "specific_date"
)

// TerminationReason represents why a subscription is being terminated.
type TerminationReason string

const (
	TerminationReasonUpgrade      TerminationReason = "upgrade"
	TerminationReasonDowngrade    TerminationReason = "downgrade"
	TerminationReasonCancellation TerminationReason = "cancellation"
	TerminationReasonExpiration   TerminationReason = "expiration"
)

// CancellationType determines when a cancellation takes effect.
type CancellationType string

const (
	CancellationTypeImmediate   CancellationType = "immediate"
	CancellationTypeEndOfPeriod CancellationType = "end_of_period"
)

var CancellationTypeValues = []CancellationType{
	CancellationTypeImmediate,
	CancellationTypeEndOfPeriod,
}

func (c CancellationType) Validate() error {
	if !lo.Contains(CancellationTypeValues, c) {
		return ierr.NewError("invalid cancellation type").
			WithHint("Cancellation type must be immediate, end_of_period, or specific_date").
			WithReportableDetails(map[string]any{
				"allowed_values": CancellationTypeValues,
				"provided_value": c,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (c CancellationType) String() string {
	return string(c)
}

// BillingCycleAnchor defines how billing cycle is handled during subscription changes
type BillingCycleAnchor string

const (
	BillingCycleAnchorUnchanged BillingCycleAnchor = "unchanged" // Keep current billing anchor
	BillingCycleAnchorReset     BillingCycleAnchor = "reset"     // Reset to current date
	BillingCycleAnchorImmediate BillingCycleAnchor = "immediate" // Bill immediately
)

var BillingCycleAnchorValues = []BillingCycleAnchor{
	BillingCycleAnchorUnchanged,
	BillingCycleAnchorReset,
	BillingCycleAnchorImmediate,
}

func (b BillingCycleAnchor) Validate() error {
	if !lo.Contains(BillingCycleAnchorValues, b) {
		return ierr.NewError("invalid billing cycle anchor").
			WithHint("Billing cycle anchor must be unchanged, reset, or immediate").
			WithReportableDetails(map[string]any{
				"allowed_values": BillingCycleAnchorValues,
				"provided_value": b,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (b BillingCycleAnchor) String() string {
	return string(b)
}

var ProrationBehaviorValues = []ProrationBehavior{
	ProrationBehaviorCreateProrations,
	ProrationBehaviorNone,
}

func (p ProrationBehavior) Validate() error {
	if !lo.Contains(ProrationBehaviorValues, p) {
		return ierr.NewError("invalid proration behavior").
			WithHint("Proration behavior must be create_prorations, always_invoice, or none").
			WithReportableDetails(map[string]any{
				"allowed_values": ProrationBehaviorValues,
				"provided_value": p,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}
