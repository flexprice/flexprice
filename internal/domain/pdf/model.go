package pdf

import (
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
)

// InvoiceData represents the data model for invoice PDF generation
type InvoiceData struct {
	Currency      string     `json:"currency"`
	Precision     int32      `json:"precision"`
	BannerImage   string     `json:"banner_image,omitempty"`
	ID            string     `json:"id"`
	InvoiceStatus string     `json:"invoice_status"`
	InvoiceNumber string     `json:"invoice_number"`
	IssuingDate   CustomTime `json:"issuing_date"`
	DueDate       CustomTime `json:"due_date"`
	AmountDue     float64    `json:"amount_due"`     // Total amount (subtotal - discount + tax)
	Subtotal      float64    `json:"subtotal"`       // Before discounts and taxes
	TotalDiscount float64    `json:"total_discount"` // Total discounts applied
	TotalTax      float64    `json:"total_tax"`      // Total tax amount
	VAT           float64    `json:"vat"`            // VAT percentage as decimal (0.18 = 18%)
	Notes         string     `json:"notes"`
	BillingReason string     `json:"billing_reason"`
	PeriodStart   CustomTime `json:"period_start"`
	PeriodEnd     CustomTime `json:"period_end"`

	// Company information
	Biller    *BillerInfo    `json:"biller"`
	Recipient *RecipientInfo `json:"recipient"`

	// Line items
	LineItems []LineItemData `json:"line_items"`

	// Applied taxes (detailed breakdown)
	AppliedTaxes []AppliedTaxData `json:"applied_taxes"`

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
}

// RecipientInfo contains customer information for the invoice recipient
type RecipientInfo struct {
	Name    string      `json:"name"`
	Email   string      `json:"email"`
	Address AddressInfo `json:"address"`
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
