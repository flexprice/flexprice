package webhook

import (
	"encoding/json"
	"fmt"
)

// RazorpayEventType represents the type of Razorpay webhook event
type RazorpayEventType string

const (
	// Payment events
	EventPaymentCaptured   RazorpayEventType = "payment.captured"
	EventPaymentFailed     RazorpayEventType = "payment.failed"
	EventPaymentAuthorized RazorpayEventType = "payment.authorized"
)

// RazorpayPaymentMethod represents the payment method used in Razorpay
type RazorpayPaymentMethod string

const (
	RazorpayPaymentMethodCard       RazorpayPaymentMethod = "card"
	RazorpayPaymentMethodUPI        RazorpayPaymentMethod = "upi"
	RazorpayPaymentMethodWallet     RazorpayPaymentMethod = "wallet"
	RazorpayPaymentMethodNetbanking RazorpayPaymentMethod = "netbanking"
	RazorpayPaymentMethodEMI        RazorpayPaymentMethod = "emi"
	RazorpayPaymentMethodCardless   RazorpayPaymentMethod = "cardless_emi"
	RazorpayPaymentMethodPaylater   RazorpayPaymentMethod = "paylater"
)

// RazorpayWebhookEvent represents a Razorpay webhook event
type RazorpayWebhookEvent struct {
	Entity    string                 `json:"entity"`
	AccountID string                 `json:"account_id"`
	Event     string                 `json:"event"`
	Contains  []string               `json:"contains"`
	Payload   RazorpayWebhookPayload `json:"payload"`
	CreatedAt int64                  `json:"created_at"`
}

// RazorpayWebhookPayload represents the payload of a Razorpay webhook
type RazorpayWebhookPayload struct {
	Payment PayloadPayment `json:"payment"`
}

// PayloadPayment represents the payment entity in the webhook payload
type PayloadPayment struct {
	Entity Payment `json:"entity"`
}

// Payment represents a Razorpay payment
type Payment struct {
	ID               string        `json:"id"`
	Entity           string        `json:"entity"`
	Amount           int64         `json:"amount"`            // Amount in smallest currency unit (paise)
	Currency         string        `json:"currency"`          // Currency code (INR, USD, etc.)
	Status           string        `json:"status"`            // created, authorized, captured, refunded, failed
	OrderID          string        `json:"order_id"`          // Razorpay order ID
	InvoiceID        string        `json:"invoice_id"`        // Razorpay invoice ID
	Method           string        `json:"method"`            // Payment method (card, netbanking, wallet, upi)
	Description      string        `json:"description"`       // Payment description
	CardID           string        `json:"card_id"`           // Card ID (if payment method is card)
	Bank             string        `json:"bank"`              // Bank name (if payment method is netbanking)
	Wallet           string        `json:"wallet"`            // Wallet name (if payment method is wallet)
	VPA              string        `json:"vpa"`               // Virtual Payment Address (if payment method is UPI)
	AmountRefunded   int64         `json:"amount_refunded"`   // Amount refunded
	Refunded         bool          `json:"refunded"`          // Whether payment is refunded
	Captured         bool          `json:"captured"`          // Whether payment is captured
	Email            string        `json:"email"`             // Customer email
	Contact          string        `json:"contact"`           // Customer contact
	Fee              int64         `json:"fee"`               // Gateway fee
	Tax              int64         `json:"tax"`               // Tax on fee
	ErrorCode        string        `json:"error_code"`        // Error code if payment failed
	ErrorDescription string        `json:"error_description"` // Error description if payment failed
	ErrorSource      string        `json:"error_source"`      // Error source
	ErrorStep        string        `json:"error_step"`        // Error step
	ErrorReason      string        `json:"error_reason"`      // Error reason
	Notes            FlexibleNotes `json:"notes"`             // Custom notes (can be object or array)
	CreatedAt        int64         `json:"created_at"`        // Unix timestamp
}

// FlexibleNotes handles both array and object formats from Razorpay
// Razorpay sometimes sends empty array [] instead of empty object {}
type FlexibleNotes map[string]interface{}

// UnmarshalJSON implements custom unmarshaling to handle both [] and {} formats
func (fn *FlexibleNotes) UnmarshalJSON(data []byte) error {
	// Try to unmarshal as object first
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err == nil {
		*fn = m
		return nil
	}

	// If that fails, it might be an array (empty [])
	// Just initialize as empty map
	var arr []interface{}
	if err := json.Unmarshal(data, &arr); err == nil {
		*fn = make(map[string]interface{})
		return nil
	}

	// If both fail, return error
	return fmt.Errorf("notes must be either object or array")
}
