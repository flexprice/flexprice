package types

import (
	"slices"
	"strings"

	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/samber/lo"
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

	// The four below are only ever set from an external engine's reason for charging
	// zero. Each means a zero for a different reason, and the difference decides whether
	// an operator needs to act.
	TaxExemptionReasonReverseCharge   TaxExemptionReasonCode = "reverse_charge"
	TaxExemptionReasonNotCollecting   TaxExemptionReasonCode = "not_collecting"
	TaxExemptionReasonNotSubjectToTax TaxExemptionReasonCode = "not_subject_to_tax"
	TaxExemptionReasonNotSupported    TaxExemptionReasonCode = "not_supported"
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
	case TaxExemptionReasonReverseCharge:
		return "Tax to be paid on reverse charge basis"
	case TaxExemptionReasonNotCollecting:
		return "Not registered to collect tax in this jurisdiction"
	case TaxExemptionReasonNotSubjectToTax:
		return "Not subject to tax in this jurisdiction"
	case TaxExemptionReasonNotSupported:
		return "Tax calculation not supported in this jurisdiction"
	default:
		return string(r)
	}
}

// ReverseChargeStatement is the wording a reverse charge invoice must carry. A zero tax
// amount on its own is not a compliant reverse charge invoice.
const ReverseChargeStatement = "Tax to be paid on reverse charge basis."

// RequiresReverseChargeStatement reports whether an invoice carrying this reason must
// print the reverse charge statement to be compliant.
func (r TaxExemptionReasonCode) RequiresReverseChargeStatement() bool {
	return r == TaxExemptionReasonReverseCharge
}

// TaxProvider names the engine that calculates tax for a tenant and environment.
// Empty means the native Flexprice engine, so no existing tenant needs a migration.
type TaxProvider string

const (
	TaxProviderFlexprice TaxProvider = "flexprice"
	TaxProviderStripe    TaxProvider = "stripe"
)

func (p TaxProvider) String() string {
	return string(p)
}

// IsExternal reports whether an engine outside Flexprice calculates the tax.
func (p TaxProvider) IsExternal() bool {
	return p != "" && p != TaxProviderFlexprice
}

// TaxProviders are the engines a tenant may name. Empty is accepted too and means the native
// engine, but it is not offered as a choice, so an engine added here is offered everywhere a
// provider is validated or reported.
var TaxProviders = []TaxProvider{TaxProviderFlexprice, TaxProviderStripe}

// ExternalTaxProviders are the engines that calculate tax outside Flexprice.
func ExternalTaxProviders() []TaxProvider {
	return lo.Filter(TaxProviders, func(p TaxProvider, _ int) bool { return p.IsExternal() })
}

func (p TaxProvider) Validate() error {
	if p == "" || slices.Contains(TaxProviders, p) {
		return nil
	}

	return ierr.NewErrorf("invalid tax provider %q", p).
		WithHintf("Tax provider must be one of: %s", JoinTaxProviders(TaxProviders)).
		WithReportableDetails(map[string]any{
			"tax_provider": p,
			"allowed":      TaxProviders,
		}).
		Mark(ierr.ErrValidation)
}

// JoinTaxProviders renders a provider list for an error a person reads.
func JoinTaxProviders(providers []TaxProvider) string {
	return strings.Join(lo.Map(providers, func(p TaxProvider, _ int) string { return string(p) }), ", ")
}

// TaxConfig names the external tax engine a tenant and environment use, if any. Its zero value
// is the native engine, so an absent setting behaves exactly as it did before the setting
// existed.
//
// Enabled is separate from Provider so an engine can be switched off without losing which one
// was configured. It gates the external engine only: native tax is what a disabled config falls
// back to, never an absence of tax.
type TaxConfig struct {
	Enabled  bool        `json:"enabled,omitempty"`
	Provider TaxProvider `json:"provider,omitempty"`
}

// UsesExternalTaxEngine reports whether an engine outside Flexprice calculates this tenant's tax.
// Every decision that turns on the setting reads this and nothing else.
func (c TaxConfig) UsesExternalTaxEngine() bool {
	return c.Enabled && c.Provider.IsExternal()
}

func (c TaxConfig) Validate() error {
	return c.Provider.Validate()
}

// TaxBehaviorSource records how a subscription-level association's tax_behavior was decided.
// Diagnostic only — it is logged at resolution, never stored.
type TaxBehaviorSource string

const (
	// TaxBehaviorSourceExplicit means the request stated the behavior.
	TaxBehaviorSourceExplicit TaxBehaviorSource = "explicit"
	// TaxBehaviorSourceDefault means the request said nothing and the behavior fell back
	// to the default.
	TaxBehaviorSourceDefault TaxBehaviorSource = "default"
)

// TaxTransactionType names what the provider transaction on this row did. Empty means a
// filing, so no existing row needs a backfill. Every engine draws the same distinction:
// Stripe reverses with create_reversal, Anrok with createNegation, Avalara with
// RefundTransaction, and each returns its own transaction for the reversal.
type TaxTransactionType string

const (
	TaxTransactionTypeFiling   TaxTransactionType = "filing"
	TaxTransactionTypeReversal TaxTransactionType = "reversal"
)

// IsReversal reports whether this row records tax being un-filed rather than filed. A reversal
// row is not a tax the entity carries, so nothing that totals or renders tax may count it.
func (t TaxTransactionType) IsReversal() bool {
	return t == TaxTransactionTypeReversal
}

const (
	// TaxReferenceVoidPrefix marks a voided invoice's reversal. Its filing already used the
	// invoice id on its own.
	TaxReferenceVoidPrefix = "void_"
)

// TaxReversalMode is how much of a filed transaction is being undone.
type TaxReversalMode string

const (
	// TaxReversalModeFull undoes the whole transaction and carries no amount.
	TaxReversalModeFull TaxReversalMode = "full"
	// TaxReversalModePartial undoes a stated gross amount, tax included.
	TaxReversalModePartial TaxReversalMode = "partial"
)

// ExternalTaxDetails is everything an external engine returned about one applied tax, frozen
// when the row is created. Null on a native row, which points at a Flexprice tax rate instead.
// A jurisdiction that imposed no tax still produces a row, so the rate fields can all be empty.
type ExternalTaxDetails struct {
	// CalculationID is the provider calculation this row was written from. Commit files it.
	CalculationID string `json:"calculation_id,omitempty"`

	// CalculationExpiresAt is when the calculation can no longer be filed, RFC3339.
	CalculationExpiresAt string `json:"calculation_expires_at,omitempty"`

	// DisplayName is the engine's own name for the tax, such as
	// "Integrated goods and services tax (IGST)".
	DisplayName string `json:"display_name,omitempty"`

	// TaxType is the tax family, such as vat, igst or sales_tax.
	TaxType string `json:"tax_type,omitempty"`

	// TaxCode is the product tax category the engine priced against, such as txcd_10000000.
	// It belongs to the line rather than the rate, so today every row on an invoice carries
	// the account default. It only varies once tax is calculated per line item.
	TaxCode string `json:"tax_code,omitempty"`

	// Percentage is the resolved rate, such as "18.0". This is what was charged, not the
	// rate that would apply if the tax were imposed.
	Percentage string `json:"percentage,omitempty"`

	// TaxabilityReason is why the engine charged what it did, such as standard_rated or
	// reverse_charge. Recorded, not yet rendered.
	TaxabilityReason string `json:"taxability_reason,omitempty"`

	// Sourcing is whether the jurisdiction followed the seller's address or the buyer's.
	Sourcing string `json:"sourcing,omitempty"`

	// Reference is what this row's provider transaction was filed under. It is unique across
	// every transaction on the account, so it is also the only way back to a transaction whose
	// id failed to land here.
	Reference string `json:"reference,omitempty"`

	// Reversal is set only on a row that records tax being un-filed.
	Reversal *TaxReversal `json:"reversal,omitempty"`

	Jurisdiction *TaxJurisdiction `json:"jurisdiction,omitempty"`
}

// TaxReversal is what a reversal row undid. The provider leaves the original transaction with no
// link to whatever reversed it, so this is the only record of the pairing.
type TaxReversal struct {
	Mode                  TaxReversalMode `json:"mode,omitempty"`
	OriginalTransactionID string          `json:"original_transaction,omitempty"`
}

// TaxJurisdiction is the authority that imposed a tax.
type TaxJurisdiction struct {
	Country     string `json:"country,omitempty"`
	State       string `json:"state,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Level       string `json:"level,omitempty"`
}

func (d *ExternalTaxDetails) GetCalculationID() string {
	if d == nil {
		return ""
	}
	return d.CalculationID
}

func (d *ExternalTaxDetails) GetCalculationExpiresAt() string {
	if d == nil {
		return ""
	}
	return d.CalculationExpiresAt
}

func (d *ExternalTaxDetails) GetDisplayName() string {
	if d == nil {
		return ""
	}
	return d.DisplayName
}

func (d *ExternalTaxDetails) GetTaxType() string {
	if d == nil {
		return ""
	}
	return d.TaxType
}

func (d *ExternalTaxDetails) GetTaxCode() string {
	if d == nil {
		return ""
	}
	return d.TaxCode
}

func (d *ExternalTaxDetails) GetPercentage() string {
	if d == nil {
		return ""
	}
	return d.Percentage
}

func (d *ExternalTaxDetails) GetTaxabilityReason() string {
	if d == nil {
		return ""
	}
	return d.TaxabilityReason
}

func (d *ExternalTaxDetails) GetReference() string {
	if d == nil {
		return ""
	}
	return d.Reference
}

func (d *ExternalTaxDetails) GetJurisdictionName() string {
	if d == nil || d.Jurisdiction == nil {
		return ""
	}
	return d.Jurisdiction.DisplayName
}

// TaxIdentifier is one registered tax number, such as an EU VAT or Indian GST number. A
// compliant VAT invoice carries the seller's and, under reverse charge, the buyer's. They are
// read from the engine when the invoice is rendered rather than stored, because the rendered
// PDF is itself kept and a regeneration is rare.
type TaxIdentifier struct {
	Type    string `json:"type,omitempty"`
	Value   string `json:"value,omitempty"`
	Country string `json:"country,omitempty"`
}
