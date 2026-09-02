package types

import (
	"slices"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
)

// TaxBehavior describes whether a tax rate is already baked into the price it applies to
// (inclusive) or added on top of it (exclusive).
type TaxBehavior string

const (
	TaxBehaviorInclusive TaxBehavior = "inclusive"
	TaxBehaviorExclusive TaxBehavior = "exclusive"
)

func (t TaxBehavior) String() string {
	return string(t)
}

func (t TaxBehavior) Validate() error {
	allowedValues := []string{string(TaxBehaviorInclusive), string(TaxBehaviorExclusive)}
	if !slices.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid tax behavior").
			WithHint("Tax behavior must be either inclusive or exclusive").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// TaxTreatment is the customer's tax treatment. Defaults to taxable.
type TaxTreatment string

const (
	TaxTreatmentTaxable TaxTreatment = "taxable"
	TaxTreatmentExempt  TaxTreatment = "exempt"
	// "reverse_charge" reserved, not implemented in v1
)

func (t TaxTreatment) String() string {
	return string(t)
}

func (t TaxTreatment) Validate() error {
	allowedValues := []string{string(TaxTreatmentTaxable), string(TaxTreatmentExempt)}
	if !slices.Contains(allowedValues, string(t)) {
		return ierr.NewError("invalid tax treatment").
			WithHint("Tax treatment must be either taxable or exempt").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// TaxExemptionReason is stored in invoices.tax_exemption_reason_code and surfaced
// as tax_summary.exemption.reason_code.
type TaxExemptionReasonCode string

const (
	TaxExemptionReasonCustomerExempt  TaxExemptionReasonCode = "customer_exempt"
	TaxExemptionReasonNoTaxConfigured TaxExemptionReasonCode = "no_tax_configured"
	// "reverse_charge" reserved, not implemented in v1
)

func (r TaxExemptionReasonCode) String() string {
	return string(r)
}

// DisplayLabel is surfaced as tax_summary.exemption.reason. Derived, never stored.
func (r TaxExemptionReasonCode) DisplayLabel() string {
	switch r {
	case TaxExemptionReasonCustomerExempt:
		return "Customer is tax exempt"
	case TaxExemptionReasonNoTaxConfigured:
		return "No tax configured"
	default:
		return string(r)
	}
}

// TaxBehaviorSource records how a subscription-level association's tax_behavior was decided.
// Diagnostic only — it is logged at resolution, never stored.
type TaxBehaviorSource string

const (
	// TaxBehaviorSourceExplicit means the request stated the behavior.
	TaxBehaviorSourceExplicit TaxBehaviorSource = "explicit"
	// TaxBehaviorSourceCurrencyDefault means the request said nothing and the behavior came
	// from the subscription currency.
	TaxBehaviorSourceCurrencyDefault TaxBehaviorSource = "currency_default"
)

// ExclusiveTaxCurrencies lists currencies whose default tax behavior is exclusive when
// an association is created without an explicit behavior. Everything else defaults to
// inclusive. Compiled-in convention — no UI, no API, no per-tenant override.
var ExclusiveTaxCurrencies = []string{"USD", "CAD"}

// DefaultTaxBehaviorForCurrency resolves the tax behavior for a subscription-level tax
// association that was not given an explicit behavior. Used only at subscription-association
// creation time — never re-resolved later.
func DefaultTaxBehaviorForCurrency(currency string) TaxBehavior {
	if slices.Contains(ExclusiveTaxCurrencies, strings.ToUpper(currency)) {
		return TaxBehaviorExclusive
	}
	return TaxBehaviorInclusive
}
