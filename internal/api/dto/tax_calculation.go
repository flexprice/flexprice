package dto

import (
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// TaxCalculationResult represents the result of tax calculations
type TaxCalculationResult struct {
	// InclusiveTax is the tax already contained in the taxable amount, recovered by working
	// backwards from it. Because it is already inside the subtotal it is never added to the
	// invoice total; it exists to report how much of the listed price was tax. Computed the
	// same way whether or not the customer is exempt.
	InclusiveTax decimal.Decimal `json:"inclusive_tax" swaggertype:"string"`

	// ExclusiveTax is added on top of the taxable amount — the only tax that moves the total.
	// Computed the same way whether or not the customer is exempt.
	ExclusiveTax decimal.Decimal `json:"exclusive_tax" swaggertype:"string"`

	// TotalTaxAmount is what is actually charged: InclusiveTax + ExclusiveTax, or zero when
	// the customer is exempt. This is what lands on invoice.total_tax.
	TotalTaxAmount decimal.Decimal `json:"total_tax_amount" swaggertype:"string"`

	// Exempt zeroes what is charged and takes the inclusive tax back out of the total.
	Exempt bool `json:"exempt"`

	TaxAppliedRecords []*TaxAppliedResponse `json:"tax_applied_records,omitempty"`

	// Provider is the engine that produced this result. Empty means the built-in engine.
	Provider types.TaxProvider `json:"provider,omitempty"`

	// ExemptionReason is why the engine charged zero, mapped to ours. Empty when tax was
	// charged normally.
	ExemptionReason *types.TaxExemptionReasonCode `json:"exemption_reason,omitempty"`

	// AmountTotal is the taxable base plus its tax, as the engine reported it rather than as
	// we would add it up. It is what a credit note owes the customer and what its reversal
	// undoes. Set only by CalculateAmount.
	AmountTotal decimal.Decimal `json:"amount_total" swaggertype:"string"`
}

// TaxCalculationRequest asks an engine what tax a taxable amount carries. An invoice and a
// credit note ask the same question, so they send the same request: only the entity, the amount
// and the reference differ.
type TaxCalculationRequest struct {
	// EntityType and EntityID are what is being taxed, and what the resulting rows are
	// stamped with.
	EntityType types.TaxRateEntityType `json:"entity_type"`
	EntityID   string                  `json:"entity_id"`

	// CustomerID is whose tax profile applies. An external engine reads the address,
	// exemption and tax IDs off its own record of them.
	CustomerID string `json:"customer_id"`

	Currency string `json:"currency"`

	// Amount is the taxable base: an invoice's subtotal net of discounts, or what a credit
	// note returns before tax.
	Amount decimal.Decimal `json:"amount" swaggertype:"string"`

	// Reference identifies this calculation in the engine's own records.
	Reference string `json:"reference"`
}

// TaxReversalRequest asks for tax an engine already recorded to be un-filed, either because a
// credit note returns part of an invoice or because a finalized invoice was voided.
type TaxReversalRequest struct {
	// EntityType and EntityID are what the reversal is recorded against: the invoice itself
	// for a void, the credit note for a credit note.
	EntityType types.TaxRateEntityType `json:"entity_type"`
	EntityID   string                  `json:"entity_id"`

	// A credit note names the invoice it credits; a void leaves this empty, because
	// the invoice being voided is the entity.
	InvoiceID string `json:"invoice_id,omitempty"`

	Mode types.TaxReversalMode `json:"mode"`

	// OriginalTransactionID is the provider transaction that filed the tax being undone. The
	// service resolves it from the invoice's rows before the engine is called.
	OriginalTransactionID string `json:"original_transaction_id"`

	// Reference identifies this reversal in the provider's tax reports and must be unique
	// across every transaction on the account, reversals included.
	Reference string `json:"reference"`

	// Amount is the gross figure being returned, tax included. Read only for a partial
	// reversal; a full one undoes the whole transaction and carries no amount.
	Amount decimal.Decimal `json:"amount" swaggertype:"string"`

	Currency string `json:"currency"`
}
