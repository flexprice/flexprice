package moyasar

import (
	"github.com/shopspring/decimal"
)

// Constants for Moyasar integration
const (
	// BaseURL is the base URL for Moyasar API
	BaseURL = "https://api.moyasar.com/v1"

	// DefaultCurrency is the default currency for Moyasar (Saudi Riyal)
	DefaultCurrency = "SAR"

	// DefaultInvoiceLabel is the default label for invoice reference
	DefaultInvoiceLabel = "Invoice"

	// SetupIntentAmount is the verification amount in halalah (1 SAR = 100 halalah)
	// This is the minimum amount used for SetupIntent/payment method verification
	// Moyasar requires amounts in halalah, so 100 = 1 SAR
	SetupIntentAmount = 100

	// SetupIntentDescription is the description for SetupIntent verification payments
	SetupIntentDescription = "Payment method verification"
)

// MoyasarConfig holds decrypted Moyasar configuration
type MoyasarConfig struct {
	PublishableKey string // For frontend use (optional)
	SecretKey      string // API secret key (required)
	WebhookSecret  string // Webhook verification secret (optional)
}

// MoyasarPaymentStatus represents Moyasar payment status values
type MoyasarPaymentStatus string

const (
	MoyasarPaymentStatusInitiated MoyasarPaymentStatus = "initiated"
	MoyasarPaymentStatusPaid      MoyasarPaymentStatus = "paid"
	MoyasarPaymentStatusFailed    MoyasarPaymentStatus = "failed"
	MoyasarPaymentStatusRefunded  MoyasarPaymentStatus = "refunded"
	MoyasarPaymentStatusVoided    MoyasarPaymentStatus = "voided"
	MoyasarPaymentStatusCaptured  MoyasarPaymentStatus = "captured"
)

// PaymentSourceType represents the type of payment source
type PaymentSourceType string

const (
	PaymentSourceTypeCreditCard PaymentSourceType = "creditcard"
	PaymentSourceTypeApplePay   PaymentSourceType = "applepay"
	PaymentSourceTypeSTCPay     PaymentSourceType = "stcpay"
	PaymentSourceTypeSamsungPay PaymentSourceType = "samsungpay"
	PaymentSourceTypeToken      PaymentSourceType = "token"
)

// PaymentSource represents the payment source in a Moyasar payment
type PaymentSource struct {
	Type        PaymentSourceType `json:"type"`
	Name        string            `json:"name,omitempty"`         // Cardholder name
	Number      string            `json:"number,omitempty"`       // Card number (masked in responses)
	Month       string            `json:"month,omitempty"`        // Expiry month
	Year        string            `json:"year,omitempty"`         // Expiry year
	CVC         string            `json:"cvc,omitempty"`          // CVC (only in requests)
	Token       string            `json:"token,omitempty"`        // Token for tokenized payments
	Mobile      string            `json:"mobile,omitempty"`       // Mobile number for STC Pay
	Company     string            `json:"company,omitempty"`      // Card company (Visa, Mastercard, etc.)
	GatewayID   string            `json:"gateway_id,omitempty"`   // Gateway ID
	ReferenceID string            `json:"reference_id,omitempty"` // Reference ID
	Message     string            `json:"message,omitempty"`      // Response message
}

// MoyasarPayment represents a Moyasar payment object
type MoyasarPayment struct {
	ID             string            `json:"id"`
	Status         string            `json:"status"`
	Amount         int               `json:"amount"`        // Amount in smallest currency unit (halalah)
	Fee            int               `json:"fee,omitempty"` // Fee amount
	Currency       string            `json:"currency"`
	RefundedAmount int               `json:"refunded,omitempty"`
	Refunded       int               `json:"refunded_at,omitempty"`
	CapturedAmount int               `json:"captured,omitempty"`
	CapturedAt     string            `json:"captured_at,omitempty"`
	VoidedAt       string            `json:"voided_at,omitempty"`
	Description    string            `json:"description,omitempty"`
	AmountFormat   string            `json:"amount_format,omitempty"`
	FeeFormat      string            `json:"fee_format,omitempty"`
	InvoiceID      string            `json:"invoice_id,omitempty"`
	IP             string            `json:"ip,omitempty"`
	CallbackURL    string            `json:"callback_url,omitempty"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Source         *PaymentSource    `json:"source,omitempty"`
}

// CreatePaymentRequest represents a request to create a Moyasar payment
type CreatePaymentRequest struct {
	Amount      int               `json:"amount"`                 // Amount in smallest currency unit
	Currency    string            `json:"currency"`               // Currency code (e.g., SAR)
	Description string            `json:"description,omitempty"`  // Payment description
	CallbackURL string            `json:"callback_url,omitempty"` // Redirect URL after payment
	Source      *PaymentSource    `json:"source,omitempty"`       // Payment source
	Metadata    map[string]string `json:"metadata,omitempty"`     // Custom metadata
	GivenID     string            `json:"given_id,omitempty"`     // Merchant UUID for idempotency
}

// CreatePaymentResponse represents the response from creating a payment
type CreatePaymentResponse struct {
	MoyasarPayment
	TransactionURL string `json:"transaction_url,omitempty"` // URL for 3DS verification
}

// CreatePaymentLinkRequest represents FlexPrice request to create a Moyasar payment
type CreatePaymentLinkRequest struct {
	InvoiceID     string
	CustomerID    string
	Amount        decimal.Decimal
	Currency      string
	SuccessURL    string // Callback URL - customer redirected here after payment
	CancelURL     string // Cancel URL (used as success URL if success not provided)
	Metadata      map[string]string
	PaymentID     string // FlexPrice payment ID
	EnvironmentID string
	Description   string
}

// CreatePaymentLinkResponse represents the response after creating a payment link
type CreatePaymentLinkResponse struct {
	ID         string          // Moyasar payment ID
	PaymentURL string          // Transaction URL for hosted payment page
	Amount     decimal.Decimal // Amount in original currency
	Currency   string          // Currency code
	Status     string          // Payment status
	CreatedAt  string          // Creation timestamp
	PaymentID  string          // FlexPrice payment ID
}

// RefundPaymentRequest represents a request to refund a payment
type RefundPaymentRequest struct {
	PaymentID string `json:"payment_id"`       // Moyasar payment ID
	Amount    int    `json:"amount,omitempty"` // Amount to refund (in smallest unit), omit for full refund
}

// RefundPaymentResponse represents the response from a refund
type RefundPaymentResponse struct {
	ID        string `json:"id"`
	PaymentID string `json:"payment_id"`
	Amount    int    `json:"amount"`
	Fee       int    `json:"fee,omitempty"`
	Currency  string `json:"currency"`
	Status    string `json:"status"`
	CreatedAt string `json:"created_at"`
}

// PaymentListResponse represents a list of payments
type PaymentListResponse struct {
	Payments []MoyasarPayment `json:"payments"`
	Meta     *ListMeta        `json:"meta,omitempty"`
}

// ListMeta represents pagination metadata
type ListMeta struct {
	CurrentPage  int `json:"current_page"`
	NextPage     int `json:"next_page,omitempty"`
	PreviousPage int `json:"previous_page,omitempty"`
	TotalPages   int `json:"total_pages"`
	TotalCount   int `json:"total_count"`
}

// ErrorResponse represents a Moyasar API error response
type ErrorResponse struct {
	Type    string            `json:"type"`
	Message string            `json:"message"`
	Errors  map[string]string `json:"errors,omitempty"`
}

// PaymentStatusResponse represents the payment status response
type PaymentStatusResponse struct {
	ID                 string            // Moyasar payment ID
	Status             string            // Payment status
	Amount             decimal.Decimal   // Amount in original currency
	Currency           string            // Currency code
	Description        string            // Payment description
	PaymentMethod      PaymentSourceType // Payment method type
	PaymentMethodID    string            // Payment method ID
	FlexPricePaymentID string            // FlexPrice payment ID (from metadata)
	CreatedAt          string            // Creation timestamp
	UpdatedAt          string            // Update timestamp
}

// MoyasarPaymentObject represents a Moyasar payment object in webhook events
// This is compatible with webhook event data structure
type MoyasarPaymentObject struct {
	ID             string            `json:"id"`
	Status         string            `json:"status"`
	Amount         int               `json:"amount"`        // Amount in smallest currency unit
	Fee            int               `json:"fee,omitempty"` // Fee amount
	Currency       string            `json:"currency"`
	RefundedAmount int               `json:"refunded,omitempty"`
	Description    string            `json:"description,omitempty"`
	InvoiceID      string            `json:"invoice_id,omitempty"`
	IP             string            `json:"ip,omitempty"`
	CallbackURL    string            `json:"callback_url,omitempty"`
	CreatedAt      string            `json:"created_at"`
	UpdatedAt      string            `json:"updated_at"`
	Metadata       map[string]string `json:"metadata,omitempty"`
	Source         *PaymentSource    `json:"source,omitempty"`
}

// ============================================================================
// Tokenization Types
// ============================================================================

// MoyasarToken represents a tokenized card in Moyasar
type MoyasarToken struct {
	ID         string            `json:"id"`
	Status     string            `json:"status"` // "created", "verified"
	Name       string            `json:"name"`   // Cardholder name
	Brand      string            `json:"brand"`  // Visa, Mastercard, mada
	Funding    string            `json:"funding,omitempty"`
	Country    string            `json:"country,omitempty"`
	Month      string            `json:"month"`
	Year       string            `json:"year"`
	Last4      string            `json:"last_four"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Message    string            `json:"message,omitempty"` // Verification message
	Verifiable bool              `json:"verifiable,omitempty"`
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
}

// CreateTokenRequest represents a request to create a Moyasar token
// This request should be made from the frontend (publishable key)
type CreateTokenRequest struct {
	Name        string            `json:"name"`                   // Cardholder name
	Number      string            `json:"number"`                 // Card number
	Month       string            `json:"month"`                  // Expiry month (MM)
	Year        string            `json:"year"`                   // Expiry year (YYYY)
	CVC         string            `json:"cvc"`                    // CVC/CVV
	SaveOnly    bool              `json:"save_only,omitempty"`    // True for tokenization without payment
	CallbackURL string            `json:"callback_url,omitempty"` // Redirect URL after 3DS
	Metadata    map[string]string `json:"metadata,omitempty"`     // Custom metadata
}

// CreateTokenResponse represents the response from creating a token
type CreateTokenResponse struct {
	MoyasarToken
	TransactionURL string `json:"transaction_url,omitempty"` // 3DS verification URL
}

// ChargeTokenRequest represents a request to charge a saved token
type ChargeTokenRequest struct {
	Amount      int               `json:"amount"`                // Amount in smallest currency unit
	Currency    string            `json:"currency"`              // Currency code
	Description string            `json:"description,omitempty"` // Payment description
	TokenID     string            `json:"-"`                     // Token ID (used in source)
	Metadata    map[string]string `json:"metadata,omitempty"`    // Custom metadata
	GivenID     string            `json:"given_id,omitempty"`    // Idempotency key
}

// ChargeTokenResponse represents the response from charging a token
type ChargeTokenResponse struct {
	MoyasarPayment
}

// TokenListResponse represents a list of tokens
type TokenListResponse struct {
	Tokens []MoyasarToken `json:"tokens"`
	Meta   *ListMeta      `json:"meta,omitempty"`
}

// SetupIntentRequest represents a request to setup a payment method (create token)
type SetupIntentRequest struct {
	CustomerID  string            // FlexPrice customer ID
	CallbackURL string            // Redirect URL after 3DS verification
	Metadata    map[string]string // Custom metadata
}

// SetupIntentResponse represents the response from setup intent
type SetupIntentResponse struct {
	TokenID        string // Moyasar token ID
	Status         string // Token status
	SetupURL       string // URL for 3DS verification (if required)
	Brand          string // Card brand
	Last4          string // Last 4 digits
	ExpiryMonth    string // Expiry month
	ExpiryYear     string // Expiry year
	CardholderName string // Cardholder name
}

// PaymentMethodInfo represents payment method information
type PaymentMethodInfo struct {
	ID          string // Token/card ID
	Type        string // "card", "mada", etc.
	Brand       string // Card brand
	Last4       string // Last 4 digits
	ExpiryMonth string // Expiry month
	ExpiryYear  string // Expiry year
	Name        string // Cardholder name
	IsDefault   bool   // Whether this is the default payment method
	CreatedAt   string // Creation timestamp
}
