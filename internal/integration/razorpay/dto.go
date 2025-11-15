package razorpay

import (
	"github.com/shopspring/decimal"
)

// Constants for Razorpay integration
const (
	// DefaultItemName is the default name for line items when no display name is available
	DefaultItemName = "Subscription"

	// DefaultInvoiceLabel is the default label for invoice reference
	DefaultInvoiceLabel = "Invoice"
)

// RazorpayConfig holds decrypted Razorpay configuration
type RazorpayConfig struct {
	KeyID         string
	SecretKey     string
	WebhookSecret string // Optional: for webhook signature verification
}

// CustomerResponse represents a Razorpay customer
type CustomerResponse struct {
	ID      string                 `json:"id"`
	Name    string                 `json:"name"`
	Email   string                 `json:"email"`
	Contact string                 `json:"contact"`
	Notes   map[string]interface{} `json:"notes"`
}

// PaymentLinkRequest represents a request to create a Razorpay payment link
type PaymentLinkRequest struct {
	Amount         int64                  `json:"amount"`          // Amount in smallest currency unit (paise for INR)
	Currency       string                 `json:"currency"`        // Currency code (e.g., INR, USD)
	Description    string                 `json:"description"`     // Description of the payment link
	Customer       *CustomerInfo          `json:"customer"`        // Customer information
	Notes          map[string]interface{} `json:"notes"`           // Additional metadata
	CallbackURL    string                 `json:"callback_url"`    // Callback URL after payment
	CallbackMethod string                 `json:"callback_method"` // HTTP method for callback (get/post)
}

// CustomerInfo represents customer information for payment link
type CustomerInfo struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Contact string `json:"contact"`
}

// PaymentLinkResponse represents a Razorpay payment link response
type PaymentLinkResponse struct {
	ID             string                 `json:"id"`
	ShortURL       string                 `json:"short_url"`
	Amount         int64                  `json:"amount"`
	AmountPaid     int64                  `json:"amount_paid"`
	Currency       string                 `json:"currency"`
	Description    string                 `json:"description"`
	Status         string                 `json:"status"` // created, partially_paid, paid, cancelled, expired
	Customer       *CustomerInfo          `json:"customer"`
	Notes          map[string]interface{} `json:"notes"`
	CreatedAt      int64                  `json:"created_at"`
	ExpireBy       int64                  `json:"expire_by,omitempty"`
	ExpiredAt      int64                  `json:"expired_at,omitempty"`
	CancelledAt    int64                  `json:"cancelled_at,omitempty"`
	RemindAt       int64                  `json:"remind_at,omitempty"`
	ReminderEnable bool                   `json:"reminder_enable"`
	Payments       []interface{}          `json:"payments"`
}

// CreatePaymentLinkRequest represents FlexPrice request to create a Razorpay payment link
type CreatePaymentLinkRequest struct {
	InvoiceID     string
	CustomerID    string
	Amount        decimal.Decimal
	Currency      string
	SuccessURL    string // Callback URL - customer redirected here after payment completion or cancellation
	CancelURL     string // Not used by Razorpay (only SuccessURL is used as callback_url)
	Metadata      map[string]string
	PaymentID     string
	EnvironmentID string
}

// RazorpayPaymentLinkResponse represents the response after creating a payment link
type RazorpayPaymentLinkResponse struct {
	ID                    string          // Razorpay payment link ID
	PaymentURL            string          // Short URL for the payment link
	Amount                decimal.Decimal // Amount in original currency
	Currency              string          // Currency code
	Status                string          // Payment link status
	CreatedAt             int64           // Unix timestamp
	PaymentID             string          // FlexPrice payment ID
	IsRazorpayInvoiceLink bool            // Whether the payment link is a Razorpay invoice link
}

// RazorpayInvoiceSyncRequest represents a request to sync FlexPrice invoice to Razorpay
type RazorpayInvoiceSyncRequest struct {
	InvoiceID string // FlexPrice invoice ID to sync
}

// RazorpayInvoiceSyncResponse represents the response after syncing invoice to Razorpay
type RazorpayInvoiceSyncResponse struct {
	RazorpayInvoiceID string          // Razorpay invoice ID
	InvoiceNumber     string          // Invoice number in Razorpay
	ShortURL          string          // Payment URL for the invoice
	Status            string          // Invoice status (draft, issued, paid, etc.)
	Amount            decimal.Decimal // Invoice total amount
	AmountDue         decimal.Decimal // Amount remaining to be paid
	Currency          string          // Currency code
	CreatedAt         int64           // Unix timestamp
}

// RazorpayLineItem represents a line item in Razorpay invoice
type RazorpayLineItem struct {
	Name        string `json:"name"`                  // Line item name
	Description string `json:"description,omitempty"` // Line item description
	Amount      int64  `json:"amount"`                // Amount in smallest currency unit
	Currency    string `json:"currency"`              // Currency code
	Quantity    int    `json:"quantity"`              // Quantity (default: 1)
}

// RazorpayInvoiceStatus represents Razorpay invoice status values
type RazorpayInvoiceStatus string

const (
	RazorpayInvoiceStatusDraft         RazorpayInvoiceStatus = "draft"
	RazorpayInvoiceStatusIssued        RazorpayInvoiceStatus = "issued"
	RazorpayInvoiceStatusPartiallyPaid RazorpayInvoiceStatus = "partially_paid"
	RazorpayInvoiceStatusPaid          RazorpayInvoiceStatus = "paid"
	RazorpayInvoiceStatusCancelled     RazorpayInvoiceStatus = "cancelled"
	RazorpayInvoiceStatusExpired       RazorpayInvoiceStatus = "expired"
	RazorpayInvoiceStatusDeleted       RazorpayInvoiceStatus = "deleted"
)
