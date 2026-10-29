package types

import (
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// BillingTermOwnerType identifies a registered collection owner.
type BillingTermOwnerType string

const BillingTermOwnerCommitment BillingTermOwnerType = "commitment"

// Validate checks the owner discriminator without restricting future adapters.
func (v BillingTermOwnerType) Validate() error {
	if strings.TrimSpace(string(v)) == "" || len(v) > 50 {
		return ierr.Validation("owner_type must contain between 1 and 50 bytes")
	}
	return nil
}

// BillingTermKind controls how many occurrences a rule generates.
type BillingTermKind string

const (
	BillingTermOnce      BillingTermKind = "once"
	BillingTermRecurring BillingTermKind = "recurring"
)

// Validate checks the rule kind.
func (v BillingTermKind) Validate() error {
	return requiredBillingEnum("kind", v, []BillingTermKind{BillingTermOnce, BillingTermRecurring})
}

// BillingTermValueType selects explicit money or the remaining collection budget.
type BillingTermValueType string

const (
	BillingTermFixed     BillingTermValueType = "fixed"
	BillingTermRemainder BillingTermValueType = "remainder"
)

// Validate checks the amount rule.
func (v BillingTermValueType) Validate() error {
	return requiredBillingEnum("value_type", v, []BillingTermValueType{BillingTermFixed, BillingTermRemainder})
}

// BillingTermTiming selects the recurring step boundary used for invoicing.
type BillingTermTiming string

const (
	BillingTermAdvance BillingTermTiming = "advance"
	BillingTermArrear  BillingTermTiming = "arrear"
)

// Validate checks recurring collection timing.
func (v BillingTermTiming) Validate() error {
	return requiredBillingEnum("bill_at", v, []BillingTermTiming{BillingTermAdvance, BillingTermArrear})
}

// BillingTermSplit weights recurring remainder occurrences.
type BillingTermSplit string

const (
	BillingTermEven         BillingTermSplit = "even"
	BillingTermProportional BillingTermSplit = "proportional"
)

// Validate checks the recurring remainder split.
func (v BillingTermSplit) Validate() error {
	return requiredBillingEnum("split", v, []BillingTermSplit{BillingTermEven, BillingTermProportional})
}

// BillingTermDelivery identifies the invoice destination for a scheduled charge.
type BillingTermDelivery string

const (
	BillingTermSubscriptionInvoice BillingTermDelivery = "subscription_invoice"
	BillingTermSeparateInvoice     BillingTermDelivery = "separate_invoice"
)

// Validate checks the invoice destination.
func (v BillingTermDelivery) Validate() error {
	return requiredBillingEnum("delivery", v, []BillingTermDelivery{BillingTermSubscriptionInvoice, BillingTermSeparateInvoice})
}

// BillingScheduleItemKind distinguishes planned collection from owner settlement.
type BillingScheduleItemKind string

const (
	BillingScheduleCharge     BillingScheduleItemKind = "charge"
	BillingScheduleSettlement BillingScheduleItemKind = "settlement"
)

// Validate checks the charge kind.
func (v BillingScheduleItemKind) Validate() error {
	return requiredBillingEnum("kind", v, []BillingScheduleItemKind{BillingScheduleCharge, BillingScheduleSettlement})
}

// BillingScheduleTrigger controls when a charge obtains its bill date.
type BillingScheduleTrigger string

const (
	BillingScheduleOnDate BillingScheduleTrigger = "on_date"
	BillingScheduleManual BillingScheduleTrigger = "manual"
)

// Validate checks bill-date resolution mode.
func (v BillingScheduleTrigger) Validate() error {
	return requiredBillingEnum("bill_trigger", v, []BillingScheduleTrigger{BillingScheduleOnDate, BillingScheduleManual})
}

// BillingScheduleStatus is independent of published/archived source visibility.
type BillingScheduleStatus string

const (
	BillingScheduleScheduled BillingScheduleStatus = "scheduled"
	BillingScheduleInvoiced  BillingScheduleStatus = "invoiced"
	BillingScheduleCanceled  BillingScheduleStatus = "canceled"
)

// Validate checks charge execution state.
func (v BillingScheduleStatus) Validate() error {
	return requiredBillingEnum("schedule_status", v, []BillingScheduleStatus{BillingScheduleScheduled, BillingScheduleInvoiced, BillingScheduleCanceled})
}

// BillingScheduleCancellationReason records why a source stopped collecting.
type BillingScheduleCancellationReason string

const (
	BillingScheduleTermsAmended  BillingScheduleCancellationReason = "terms_amended"
	BillingScheduleInvoiceVoided BillingScheduleCancellationReason = "invoice_voided"
	BillingScheduleTermination   BillingScheduleCancellationReason = "termination"
)

// Validate checks a present cancellation reason.
func (v BillingScheduleCancellationReason) Validate() error {
	return requiredBillingEnum("cancellation_reason", v, []BillingScheduleCancellationReason{BillingScheduleTermsAmended, BillingScheduleInvoiceVoided, BillingScheduleTermination})
}

func requiredBillingEnum[T ~string](field string, value T, allowed []T) error {
	if value == "" {
		return ierr.Validationf("%s is required", field)
	}
	return validateEnum(field, value, allowed)
}
