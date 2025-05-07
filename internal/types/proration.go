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
	ProrationBehaviorAlwaysInvoice    ProrationBehavior = "always_invoice"    // Always create invoice items
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

// ProrationMode determines how proration is applied.
type ProrationMode string

const (
	ProrationModeNone   ProrationMode = "none"
	ProrationModeActive ProrationMode = "active"
)

var ProrationModeValues = []ProrationMode{
	ProrationModeActive,
	ProrationModeNone,
}

func (p ProrationMode) Validate() error {
	if !lo.Contains(ProrationModeValues, p) {
		return ierr.NewError("invalid proration mode").
			WithHint("Proration mode must be either active or none").
			WithReportableDetails(map[string]any{
				"allowed_values": []ProrationMode{ProrationModeActive, ProrationModeNone},
				"provided_value": p,
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (p ProrationMode) String() string {
	return string(p)
}
