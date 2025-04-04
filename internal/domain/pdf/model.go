package pdf

import (
	"encoding/json"
	"time"
)

// InvoiceData represents the data model for invoice PDF generation
type InvoiceData struct {
	Currency      string     `json:"currency"`
	BannerImage   string     `json:"banner_image,omitempty"`
	ID            string     `json:"id"`
	InvoiceStatus string     `json:"invoice_status"`
	InvoiceNumber string     `json:"invoice_number"`
	IssuingDate   CustomTime `json:"issuing_date"`
	DueDate       CustomTime `json:"due_date"`
	AmountDue     float64    `json:"amount_due"`
	VAT           float64    `json:"vat"` // VAT percentage as decimal (0.18 = 18%)
	Notes         string     `json:"notes"`
	BillingReason string     `json:"billing_reason"`

	// Company information
	Biller    *BillerInfo    `json:"biller"`
	Recipient *RecipientInfo `json:"recipient"`

	// Line items
	LineItems []LineItemData `json:"line_items"`
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
	PlanDisplayName string     `json:"plan_display_name"`
	DisplayName     string     `json:"display_name"`
	Description     string     `json:"description"`
	PeriodStart     CustomTime `json:"period_start"`
	PeriodEnd       CustomTime `json:"period_end"`
	Amount          float64    `json:"amount"`
	Quantity        float64    `json:"quantity"`
	Currency        string     `json:"currency"`
}

type CustomTime struct {
	time.Time
}

func (ct CustomTime) MarshalJSON() ([]byte, error) {
	return json.Marshal(ct.Format("2006-01-02")) // Format to YYYY-MM-DD
}
