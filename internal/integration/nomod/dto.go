package nomod

import (
	"github.com/shopspring/decimal"
)

// Constants for Nomod integration
const (
	// NomodBaseURL is the base URL for Nomod API
	NomodBaseURL = "https://api.nomod.com"

	// DefaultItemName is the default name for line items when no display name is available
	DefaultItemName = "Subscription"

	// DefaultInvoiceLabel is the default label for invoice reference
	DefaultInvoiceLabel = "Invoice"
)

// NomodConfig holds decrypted Nomod configuration
type NomodConfig struct {
	APIKey        string // API Key for authentication
	WebhookSecret string // Webhook secret for basic auth (optional)
}

// CustomerResponse represents a Nomod customer response
type CustomerResponse struct {
	ID           string `json:"id"`
	Created      string `json:"created"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	BusinessName string `json:"business_name"`
	JobTitle     string `json:"job_title"`
	Province     int    `json:"province"`
	City         string `json:"city"`
	Street       string `json:"street"`
	ZipCode      string `json:"zip_code"`
	Country      string `json:"country"`
	Email        string `json:"email"`
	PhoneNumber  string `json:"phone_number"`
}

// CreateCustomerRequest represents a request to create a Nomod customer
type CreateCustomerRequest struct {
	FirstName    string  `json:"first_name"`              // Required, non-empty, <= 150 chars
	LastName     *string `json:"last_name,omitempty"`     // Optional, <= 150 chars
	Email        string  `json:"email"`                   // Email format
	PhoneNumber  *string `json:"phone_number,omitempty"`  // Optional, <= 15 chars
	BusinessName *string `json:"business_name,omitempty"` // Optional, <= 50 chars
	JobTitle     *string `json:"job_title,omitempty"`     // Optional, <= 50 chars
	Country      *string `json:"country,omitempty"`       // Optional, country code, <= 2 chars
	Province     *string `json:"province,omitempty"`      // Optional, province UUID
	Street       *string `json:"street,omitempty"`        // Optional, <= 100 chars
	City         *string `json:"city,omitempty"`          // Optional, <= 100 chars
	ZipCode      *string `json:"zip_code,omitempty"`      // Optional, <= 16 chars
}

// LineItem represents a line item in a payment link or invoice
type LineItem struct {
	Name     string `json:"name"`     // Item name
	Amount   string `json:"amount"`   // Decimal string
	Quantity int    `json:"quantity"` // Quantity
}

// CustomField represents a custom field
type CustomField struct {
	Name string `json:"name"`
}

// PaymentLinkResponse represents a Nomod payment link response
type PaymentLinkResponse struct {
	ID                   string        `json:"id"`
	ReferenceID          string        `json:"reference_id"`
	Title                string        `json:"title"`
	URL                  string        `json:"url"`
	Amount               string        `json:"amount"`
	Currency             string        `json:"currency"`
	Status               string        `json:"status"`
	Discount             string        `json:"discount"`
	ServiceFee           string        `json:"service_fee"`
	AllowTip             bool          `json:"allow_tip"`
	ShippingAddressReq   bool          `json:"shipping_address_required"`
	Items                []interface{} `json:"items"`
	Tax                  string        `json:"tax"`
	Note                 string        `json:"note"`
	Taxes                []interface{} `json:"taxes"`
	DiscountPercentage   int           `json:"discount_percentage"`
	ServiceFeePercentage string        `json:"service_fee_percentage"`
	Tip                  string        `json:"tip"`
	TipPercentage        int           `json:"tip_percentage"`
	DueDate              *string       `json:"due_date,omitempty"`
	CustomFields         []interface{} `json:"custom_fields"`
	Source               string        `json:"source"`
	AllowTabby           bool          `json:"allow_tabby"`
	AllowTamara          bool          `json:"allow_tamara"`
	AllowServiceFee      bool          `json:"allow_service_fee"`
	PaymentExpiryLimit   *int          `json:"payment_expiry_limit,omitempty"`
	ExpiryDate           *string       `json:"expiry_date,omitempty"`
}

// CreatePaymentLinkRequest represents a request to create a Nomod payment link
type CreatePaymentLinkRequest struct {
	Currency                string        `json:"currency"`                            // Required, ISO 4217, <= 3 chars
	Items                   []LineItem    `json:"items"`                               // Required
	Title                   *string       `json:"title,omitempty"`                     // Optional, <= 50 chars
	Note                    *string       `json:"note,omitempty"`                      // Optional, <= 280 chars
	DiscountPercentage      *int          `json:"discount_percentage,omitempty"`       // Optional, 0-100, default 0
	ShippingAddressRequired *bool         `json:"shipping_address_required,omitempty"` // Optional, default false
	AllowTip                *bool         `json:"allow_tip,omitempty"`                 // Optional, default false
	CustomFields            []CustomField `json:"custom_fields,omitempty"`             // Optional
	SuccessURL              *string       `json:"success_url,omitempty"`               // Optional URI
	FailureURL              *string       `json:"failure_url,omitempty"`               // Optional URI
	AllowTabby              *bool         `json:"allow_tabby,omitempty"`               // Optional, default true
	AllowTamara             *bool         `json:"allow_tamara,omitempty"`              // Optional, default true
	AllowServiceFee         *bool         `json:"allow_service_fee,omitempty"`         // Optional, default true
	PaymentExpiryLimit      *int          `json:"payment_expiry_limit,omitempty"`      // Optional, >= 1
	ExpiryDate              *string       `json:"expiry_date,omitempty"`               // Optional, date format
}

// InvoiceResponse represents a Nomod invoice response
type InvoiceResponse struct {
	ID                   string        `json:"id"`
	ReferenceID          string        `json:"reference_id"`
	Created              string        `json:"created"`
	Title                string        `json:"title"`
	Code                 string        `json:"code"`
	URL                  string        `json:"url"`
	Amount               string        `json:"amount"`
	Currency             string        `json:"currency"`
	Status               string        `json:"status"` // paid, unpaid, overdue, cancelled, scheduled
	Discount             string        `json:"discount"`
	ServiceFee           *string       `json:"service_fee,omitempty"`
	Tax                  string        `json:"tax"`
	ShippingAddressReq   bool          `json:"shipping_address_required"`
	Items                []interface{} `json:"items"`
	Note                 string        `json:"note"`
	Expiry               string        `json:"expiry"`
	DiscountPercentage   *string       `json:"discount_percentage,omitempty"`
	ServiceFeePercentage *string       `json:"service_fee_percentage,omitempty"`
	TipPercentage        *string       `json:"tip_percentage,omitempty"`
	User                 interface{}   `json:"user"`
	Tip                  string        `json:"tip"`
	InvoiceNumber        string        `json:"invoice_number"`
	ServiceDate          string        `json:"service_date"`
	DueDate              string        `json:"due_date"`
	IntervalCount        int           `json:"interval_count"`
	StartsAt             string        `json:"starts_at"`
	EndsAt               string        `json:"ends_at"`
	DueDays              int           `json:"due_days"`
	CustomFields         []interface{} `json:"custom_fields"`
	Source               string        `json:"source"`
	SuccessURL           *string       `json:"success_url,omitempty"`
	FailureURL           *string       `json:"failure_url,omitempty"`
	PaymentCaptureSource *string       `json:"payment_capture_source,omitempty"`
	Files                []interface{} `json:"files"`
	Reminders            []interface{} `json:"reminders"`
	Events               []interface{} `json:"events"`
	Customer             interface{}   `json:"customer"`
}

// CreateInvoiceRequest represents a request to create a Nomod invoice
type CreateInvoiceRequest struct {
	Currency                string        `json:"currency"`                            // Required, ISO 4217, <= 3 chars
	Items                   []LineItem    `json:"items"`                               // Required
	DiscountPercentage      *int          `json:"discount_percentage,omitempty"`       // Optional, 0-100, default 0
	Customer                string        `json:"customer"`                            // Required, customer UUID
	InvoiceNumber           *string       `json:"invoice_number,omitempty"`            // Optional, <= 30 chars
	Title                   *string       `json:"title,omitempty"`                     // Optional, <= 50 chars
	Note                    *string       `json:"note,omitempty"`                      // Optional, <= 280 chars
	ShippingAddressRequired *bool         `json:"shipping_address_required,omitempty"` // Optional, default false
	DueDate                 string        `json:"due_date"`                            // Required, date format
	StartsAt                *string       `json:"starts_at,omitempty"`                 // Optional, date format
	CustomFields            []CustomField `json:"custom_fields,omitempty"`             // Optional
	AllowTabby              *bool         `json:"allow_tabby,omitempty"`               // Optional, default true
	AllowTamara             *bool         `json:"allow_tamara,omitempty"`              // Optional, default true
	AllowServiceFee         *bool         `json:"allow_service_fee,omitempty"`         // Optional, default true
	SuccessURL              *string       `json:"success_url,omitempty"`               // Optional URI
	FailureURL              *string       `json:"failure_url,omitempty"`               // Optional URI
}

// NomodInvoiceSyncRequest represents a request to sync FlexPrice invoice to Nomod
type NomodInvoiceSyncRequest struct {
	InvoiceID string // FlexPrice invoice ID to sync
}

// NomodInvoiceSyncResponse represents the response after syncing invoice to Nomod
type NomodInvoiceSyncResponse struct {
	NomodInvoiceID string          // Nomod invoice ID
	ReferenceID    string          // Nomod reference ID
	InvoiceNumber  string          // Invoice number in Nomod
	Code           string          // Nomod invoice code
	URL            string          // Payment URL for the invoice
	Status         string          // Invoice status (paid, unpaid, overdue, cancelled, scheduled)
	Amount         decimal.Decimal // Invoice total amount
	Currency       string          // Currency code
	CreatedAt      string          // Created timestamp
}

// NomodInvoiceStatus represents Nomod invoice status values
type NomodInvoiceStatus string

const (
	NomodInvoiceStatusPaid      NomodInvoiceStatus = "paid"
	NomodInvoiceStatusUnpaid    NomodInvoiceStatus = "unpaid"
	NomodInvoiceStatusOverdue   NomodInvoiceStatus = "overdue"
	NomodInvoiceStatusCancelled NomodInvoiceStatus = "cancelled"
	NomodInvoiceStatusScheduled NomodInvoiceStatus = "scheduled"
)

// ChargeResponse represents Nomod charge details from GET /v1/charges/{id}
type ChargeResponse struct {
	ID                   string                 `json:"id"`
	ReferenceID          int                    `json:"reference_id"`
	Created              string                 `json:"created"`
	Items                []ChargeItem           `json:"items"`
	Currency             string                 `json:"currency"`
	Discount             string                 `json:"discount"`
	ServiceFee           string                 `json:"service_fee"`
	Status               string                 `json:"status"` // paid, pending, failed, etc.
	Tip                  string                 `json:"tip"`
	Tax                  string                 `json:"tax"`
	Total                string                 `json:"total"`
	RefundTotal          string                 `json:"refund_total"`
	Note                 string                 `json:"note"`
	PaymentMethod        string                 `json:"payment_method"`
	DiscountPercentage   *int                   `json:"discount_percentage"`
	ServiceFeePercentage *string                `json:"service_fee_percentage"`
	TipPercentage        string                 `json:"tip_percentage"`
	User                 ChargeUser             `json:"user"`
	CustomFields         interface{}            `json:"custom_fields"`
	Refunds              []interface{}          `json:"refunds"`
	Events               []ChargeEvent          `json:"events"`
	SettlementCurrency   string                 `json:"settlement_currency"`
	Fee                  string                 `json:"fee"`
	FxFee                string                 `json:"fx_fee"`
	FeeTax               string                 `json:"fee_tax"`
	Net                  string                 `json:"net"`
	NetworkCost          ChargeNetworkCost      `json:"network_cost"`
	SuccessURL           string                 `json:"success_url"`
	FailureURL           *string                `json:"failure_url"`
	ShippingAddress      *ChargeShippingAddress `json:"shipping_address"`
	CustomerInfo         ChargeCustomerInfo     `json:"customer_info"`
	Link                 *ChargeLink            `json:"link,omitempty"`
	Source               string                 `json:"source"`
	IconURL              string                 `json:"icon_url"`
	FeeBreakdown         ChargeFeeBreakdown     `json:"fee_breakdown"`
	Customer             ChargeCustomer         `json:"customer"`
}

// ChargeItem represents an item in the charge
type ChargeItem struct {
	Amount      string      `json:"amount"`
	Name        string      `json:"name"`
	TotalAmount string      `json:"total_amount"`
	Quantity    int         `json:"quantity"`
	Product     interface{} `json:"product"`
	SKU         interface{} `json:"sku"`
}

// ChargeUser represents the user who made the payment
type ChargeUser struct {
	ID        string `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// ChargeEvent represents an event in the charge history
type ChargeEvent struct {
	Created string      `json:"created"`
	Type    string      `json:"type"`
	Message string      `json:"message"`
	User    *ChargeUser `json:"user"`
}

// ChargeNetworkCost represents network cost details
type ChargeNetworkCost struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

// ChargeShippingAddress represents shipping address (if any)
type ChargeShippingAddress struct {
	// Define fields if needed
}

// ChargeCustomerInfo represents customer information from the charge
type ChargeCustomerInfo struct {
	FirstName   string `json:"first_name"`
	LastName    string `json:"last_name"`
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
}

// ChargeLink represents payment link details (if payment was made via link)
type ChargeLink struct {
	ID          string `json:"id"`
	ReferenceID string `json:"reference_id"`
	Status      string `json:"status"`
}

// ChargeFeeBreakdown represents fee breakdown
type ChargeFeeBreakdown struct {
	Currency string          `json:"currency"`
	Amount   string          `json:"amount"`
	Fees     []ChargeFeeItem `json:"fees"`
}

// ChargeFeeItem represents individual fee item
type ChargeFeeItem struct {
	Currency    string   `json:"currency"`
	Amount      string   `json:"amount"`
	Description string   `json:"description"`
	Details     []string `json:"details"`
}

// ChargeCustomer represents the customer in Nomod
type ChargeCustomer struct {
	ID           string `json:"id"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	BusinessName string `json:"business_name"`
	Email        string `json:"email"`
}
