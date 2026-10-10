package pdf

import (
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
)

// InvoiceData represents the data model for invoice PDF generation
type InvoiceData struct {
	Currency                   string     `json:"currency"`
	Precision                  int32      `json:"precision"`
	BannerImage                string     `json:"banner_image,omitempty"`
	ID                         string     `json:"id"`
	InvoiceStatus              string     `json:"invoice_status"`
	InvoiceNumber              string     `json:"invoice_number"`
	PONumber                   string     `json:"po_number,omitempty"`
	IssuingDate                CustomTime `json:"issuing_date"`
	DueDate                    CustomTime `json:"due_date"`
	AmountDue                  float64    `json:"amount_due"`                    // Total amount (subtotal - discount + tax)
	Subtotal                   float64    `json:"subtotal"`                      // Before discounts and taxes
	TotalDiscount              float64    `json:"total_discount"`                // Total discounts applied
	TotalPrepaidCreditsApplied float64    `json:"total_prepaid_credits_applied"` // Total prepaid credits applied
	TotalTax                   float64    `json:"total_tax"`                     // Total tax amount
	VAT                        float64    `json:"vat"`                           // VAT percentage as decimal (0.18 = 18%)
	Notes                      string     `json:"notes"`
	BillingReason              string     `json:"billing_reason"`
	PeriodStart                CustomTime `json:"period_start"`
	PeriodEnd                  CustomTime `json:"period_end"`
	BillingPeriod              string     `json:"billing_period,omitempty"`
	Description                string     `json:"description,omitempty"`
	AmountPaid                 float64    `json:"amount_paid,omitempty"`
	AmountRemaining            float64    `json:"amount_remaining,omitempty"`
	PaymentStatus              string     `json:"payment_status,omitempty"`
	InvoiceType                string     `json:"invoice_type,omitempty"`

	// Company information
	Biller    *BillerInfo    `json:"biller"`
	Recipient *RecipientInfo `json:"recipient"`

	// Line items
	LineItems []LineItemData `json:"line_items"`

	// Applied taxes (detailed breakdown)
	AppliedTaxes []AppliedTaxData `json:"applied_taxes"`

	// TaxNotice is a statement the invoice must carry to be compliant, such as the reverse
	// charge wording when the buyer accounts for the tax. Empty when none applies.
	TaxNotice string `json:"tax_notice,omitempty"`

	// Applied discounts (detailed breakdown)
	AppliedDiscounts []AppliedDiscountData `json:"applied_discounts"`
}

// BillerInfo contains company information for the invoice issuer
type BillerInfo struct {
	Name                string      `json:"name"`
	Email               string      `json:"email"`
	Website             string      `json:"website"`
	HelpEmail           string      `json:"help_email"`
	PaymentInstructions string      `json:"payment_instructions"`
	Address             AddressInfo `json:"address"`

	// TaxIDs are the issuer's registered tax numbers. A compliant VAT invoice carries them.
	TaxIDs []TaxIDData `json:"tax_ids,omitempty"`
}

// TaxIDData is one registered tax number rendered on the invoice.
type TaxIDData struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// RecipientInfo contains customer information for the invoice recipient
type RecipientInfo struct {
	Name    string      `json:"name"`
	Email   string      `json:"email"`
	Address AddressInfo `json:"address"`

	// TaxIDs are the customer's registered tax numbers. Required on a reverse charge invoice,
	// where the customer accounts for the tax themselves.
	TaxIDs []TaxIDData `json:"tax_ids,omitempty"`
}

// AddressInfo represents a physical address
type AddressInfo struct {
	Street     string `json:"street"`
	City       string `json:"city"`
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
}

// LineItemData represents an invoice line item for PDF generation
type LineItemData struct {
	PlanDisplayName string                   `json:"plan_display_name"`
	DisplayName     string                   `json:"display_name"`
	Description     string                   `json:"description"`
	Group           string                   `json:"group"` // Group name if the price belongs to a group, "--" if no group
	PeriodStart     CustomTime               `json:"period_start"`
	PeriodEnd       CustomTime               `json:"period_end"`
	Amount          float64                  `json:"amount"` // Positive for charges, negative for discounts
	Quantity        float64                  `json:"quantity"`
	Currency        string                   `json:"currency"`
	Type            string                   `json:"type"` // "subscription", "addon", "discount", "tax"
	UsageBreakdown  []dto.UsageBreakdownItem `json:"usage_breakdown,omitempty"`
}

// AppliedTaxData represents a tax applied to the invoice
type AppliedTaxData struct {
	TaxName       string  `json:"tax_name"`
	TaxCode       string  `json:"tax_code"`
	TaxType       string  `json:"tax_type"`       // "Fixed" or "Percentage"
	TaxRate       float64 `json:"tax_rate"`       // Rate value (e.g., 1.00 for fixed, 10.0 for 10%)
	TaxableAmount float64 `json:"taxable_amount"` // Amount tax was calculated on
	TaxAmount     float64 `json:"tax_amount"`     // Actual tax amount

	// Jurisdiction is the authority that imposed the tax, e.g. "India" or "Colorado". Set by
	// an external engine only; it is what tells apart several rows of the same tax.
	Jurisdiction string `json:"jurisdiction,omitempty"`
	// AppliedAt     string  `json:"applied_at"`     // Date when tax was applied
}

// AppliedDiscountData represents a discount applied to the invoice
type AppliedDiscountData struct {
	DiscountName   string  `json:"discount_name"`   // Human-readable discount name
	Type           string  `json:"type"`            // "Fixed" or "Percentage"
	Value          float64 `json:"value"`           // Discount value (e.g., 5.00 for $5 off, 10.0 for 10% off)
	DiscountAmount float64 `json:"discount_amount"` // Actual discount amount applied
	LineItemRef    string  `json:"line_item_ref"`   // Line item reference if discount applied to specific line item, empty if invoice-level
}

type CustomTime struct {
	time.Time
}

func (ct CustomTime) MarshalJSON() ([]byte, error) {
	if ct.IsZero() {
		return json.Marshal("")
	}
	return json.Marshal(ct.Format("2006-01-02")) // Format to YYYY-MM-DD
}
