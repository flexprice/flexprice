package webhook

// MoyasarEventType represents Moyasar webhook event types
type MoyasarEventType string

const (
	// Payment events
	EventPaymentPaid     MoyasarEventType = "payment_paid"
	EventPaymentFailed   MoyasarEventType = "payment_failed"
	EventPaymentRefunded MoyasarEventType = "payment_refunded"
	EventPaymentVoided   MoyasarEventType = "payment_voided"
	EventPaymentCaptured MoyasarEventType = "payment_captured"
)

// MoyasarWebhookEvent represents a Moyasar webhook event payload
type MoyasarWebhookEvent struct {
	ID        string           `json:"id"`
	Type      MoyasarEventType `json:"type"`
	CreatedAt string           `json:"created_at"`
	Data      PaymentEventData `json:"data"`
	Secret    string           `json:"secret,omitempty"` // Webhook secret token for verification
}

// PaymentEventData represents the data object in a payment webhook
type PaymentEventData struct {
	ID             string             `json:"id"`
	Status         string             `json:"status"`
	Amount         int                `json:"amount"` // Amount in smallest currency unit
	Fee            int                `json:"fee,omitempty"`
	Currency       string             `json:"currency"`
	RefundedAmount int                `json:"refunded,omitempty"`
	Description    string             `json:"description,omitempty"`
	AmountFormat   string             `json:"amount_format,omitempty"`
	FeeFormat      string             `json:"fee_format,omitempty"`
	InvoiceID      string             `json:"invoice_id,omitempty"`
	IP             string             `json:"ip,omitempty"`
	CallbackURL    string             `json:"callback_url,omitempty"`
	CreatedAt      string             `json:"created_at"`
	UpdatedAt      string             `json:"updated_at"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
	Source         *PaymentSourceData `json:"source,omitempty"`
}

// PaymentSourceData represents payment source information in webhook
type PaymentSourceData struct {
	Type        string `json:"type"`
	Company     string `json:"company,omitempty"`      // Card company (Visa, Mastercard, etc.)
	Name        string `json:"name,omitempty"`         // Cardholder name
	Number      string `json:"number,omitempty"`       // Masked card number
	GatewayID   string `json:"gateway_id,omitempty"`   // Gateway ID
	ReferenceID string `json:"reference_id,omitempty"` // Reference ID
	Message     string `json:"message,omitempty"`      // Response message
}

// MoyasarPaymentMethod represents Moyasar payment method types
type MoyasarPaymentMethod string

const (
	MoyasarPaymentMethodCard       MoyasarPaymentMethod = "creditcard"
	MoyasarPaymentMethodApplePay   MoyasarPaymentMethod = "applepay"
	MoyasarPaymentMethodSTCPay     MoyasarPaymentMethod = "stcpay"
	MoyasarPaymentMethodSamsungPay MoyasarPaymentMethod = "samsungpay"
)
