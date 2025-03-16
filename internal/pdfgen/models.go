package pdfgen

import (
	"time"

	"github.com/shopspring/decimal"
)

// InvoiceData represents the data model for invoice PDF generation
type InvoiceData struct {
	ID              string          `json:"id"`
	InvoiceNumber   string          `json:"invoice_number"`
	CustomerID      string          `json:"customer_id"`
	SubscriptionID  string          `json:"subscription_id,omitempty"`
	InvoiceType     string          `json:"invoice_type"`
	InvoiceStatus   string          `json:"invoice_status"`
	PaymentStatus   string          `json:"payment_status"`
	Currency        string          `json:"currency"`
	AmountDue       decimal.Decimal `json:"amount_due"`
	AmountPaid      decimal.Decimal `json:"amount_paid"`
	AmountRemaining decimal.Decimal `json:"amount_remaining"`
	Description     string          `json:"description,omitempty"`
	DueDate         time.Time       `json:"due_date,omitempty"`
	PaidAt          *time.Time      `json:"paid_at,omitempty"`
	VoidedAt        *time.Time      `json:"voided_at,omitempty"`
	FinalizedAt     *time.Time      `json:"finalized_at,omitempty"`
	PeriodStart     *time.Time      `json:"period_start,omitempty"`
	PeriodEnd       *time.Time      `json:"period_end,omitempty"`
	InvoicePdfURL   string          `json:"invoice_pdf_url,omitempty"`
	BillingReason   string          `json:"billing_reason,omitempty"`
	Notes           string          `json:"notes,omitempty"`
	VAT             float64         `json:"vat"` // VAT percentage as decimal (0.18 = 18%)
	
	// Company information
	Biller    BillerInfo    `json:"biller"`
	Recipient RecipientInfo `json:"recipient"`
	
	// Line items
	LineItems []LineItemData `json:"line_items"`
}

// BillerInfo contains company information for the invoice issuer
type BillerInfo struct {
	Name              string        `json:"name"`
	Email             string        `json:"email,omitempty"`
	Website           string        `json:"website,omitempty"`
	HelpEmail         string        `json:"help_email,omitempty"`
	PaymentInstructions string      `json:"payment_instructions,omitempty"`
	Address           AddressInfo   `json:"address"`
}

// RecipientInfo contains customer information for the invoice recipient
type RecipientInfo struct {
	Name    string      `json:"name"`
	Email   string      `json:"email,omitempty"`
	Address AddressInfo `json:"address"`
}

// AddressInfo represents a physical address
type AddressInfo struct {
	Street     string `json:"street"`
	City       string `json:"city"`
	State      string `json:"state,omitempty"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country,omitempty"`
}

// LineItemData represents an invoice line item for PDF generation
type LineItemData struct {
	PlanDisplayName  string          `json:"plan_display_name"`
	DisplayName      string          `json:"display_name"`
	PeriodStart      *time.Time      `json:"period_start,omitempty"`
	PeriodEnd        *time.Time      `json:"period_end,omitempty"`
	Amount           decimal.Decimal `json:"amount"`
	Quantity         decimal.Decimal `json:"quantity"`
	Currency         string          `json:"currency"`
}

// PDFRequest represents a request to generate a PDF for an invoice
type PDFRequest struct {
	InvoiceID string `json:"invoice_id"`
}

// PDFResponse represents the response from a PDF generation request
type PDFResponse struct {
	InvoiceID   string `json:"invoice_id"`
	PDFLocation string `json:"pdf_location"`
	Error       string `json:"error,omitempty"`
}
