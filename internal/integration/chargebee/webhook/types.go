package webhook

import (
	"encoding/json"
	"time"
)

// ChargebeeWebhookEvent represents the structure of a Chargebee webhook event
type ChargebeeWebhookEvent struct {
	ID            string          `json:"id"`
	OccurredAt    int64           `json:"occurred_at"`
	Source        string          `json:"source"`
	Object        string          `json:"object"`
	APIVersion    string          `json:"api_version"`
	EventType     string          `json:"event_type"`
	WebhookStatus string          `json:"webhook_status"`
	Content       json.RawMessage `json:"content"`
}

// ChargebeeWebhookContent represents the content field of a webhook event
type ChargebeeWebhookContent struct {
	Transaction  *ChargebeeTransaction  `json:"transaction,omitempty"`
	Invoice      *ChargebeeInvoice      `json:"invoice,omitempty"`
	Customer     *ChargebeeCustomer     `json:"customer,omitempty"`
	Subscription *ChargebeeSubscription `json:"subscription,omitempty"`
}

// ChargebeeTransaction represents a transaction in the webhook content
type ChargebeeTransaction struct {
	ID               string `json:"id"`
	CustomerID       string `json:"customer_id"`
	SubscriptionID   string `json:"subscription_id,omitempty"`
	PaymentMethod    string `json:"payment_method"`
	ReferenceNumber  string `json:"reference_number,omitempty"`
	GatewayAccountID string `json:"gateway_account_id,omitempty"`
	Type             string `json:"type"`
	Date             int64  `json:"date"`
	CurrencyCode     string `json:"currency_code"`
	Amount           int64  `json:"amount"`
	AmountUnused     int64  `json:"amount_unused"`
	Status           string `json:"status"`
	LinkedInvoices   []struct {
		InvoiceID     string `json:"invoice_id"`
		AppliedAmount int64  `json:"applied_amount"`
		AppliedAt     int64  `json:"applied_at"`
		InvoiceDate   int64  `json:"invoice_date"`
		InvoiceTotal  int64  `json:"invoice_total"`
		InvoiceStatus string `json:"invoice_status"`
	} `json:"linked_invoices,omitempty"`
}

// ChargebeeInvoice represents an invoice in the webhook content
type ChargebeeInvoice struct {
	ID             string                     `json:"id"`
	CustomerID     string                     `json:"customer_id"`
	SubscriptionID string                     `json:"subscription_id,omitempty"`
	CurrencyCode   string                     `json:"currency_code"`
	Total          int64                      `json:"total"`
	AmountPaid     int64                      `json:"amount_paid"`
	AmountAdjusted int64                      `json:"amount_adjusted"`
	AmountDue      int64                      `json:"amount_due"`
	Status         string                     `json:"status"`
	Date           int64                      `json:"date"`
	DueDate        int64                      `json:"due_date,omitempty"`
	NetTermDays    int                        `json:"net_term_days,omitempty"`
	PriceType      string                     `json:"price_type"`
	LineItems      []ChargebeeInvoiceLineItem `json:"line_items,omitempty"`
	LinkedPayments []struct {
		TransactionID     string `json:"txn_id"`
		AppliedAmount     int64  `json:"applied_amount"`
		AppliedAt         int64  `json:"applied_at"`
		TransactionStatus string `json:"txn_status,omitempty"`
	} `json:"linked_payments,omitempty"`
}

// ChargebeeInvoiceLineItem represents a line item in an invoice
type ChargebeeInvoiceLineItem struct {
	ID                      string `json:"id"`
	SubscriptionID          string `json:"subscription_id,omitempty"`
	DateFrom                int64  `json:"date_from"`
	DateTo                  int64  `json:"date_to"`
	UnitAmount              int64  `json:"unit_amount"`
	Quantity                int    `json:"quantity,omitempty"`
	Amount                  int64  `json:"amount"`
	PricingModel            string `json:"pricing_model,omitempty"`
	IsTaxed                 bool   `json:"is_taxed"`
	TaxAmount               int64  `json:"tax_amount,omitempty"`
	TaxRate                 int64  `json:"tax_rate,omitempty"`
	EntityType              string `json:"entity_type"`
	EntityID                string `json:"entity_id"`
	Description             string `json:"description,omitempty"`
	DiscountAmount          int64  `json:"discount_amount,omitempty"`
	ItemLevelDiscountAmount int64  `json:"item_level_discount_amount,omitempty"`
}

// ChargebeeCustomer represents a customer in the webhook content
type ChargebeeCustomer struct {
	ID             string `json:"id"`
	FirstName      string `json:"first_name,omitempty"`
	LastName       string `json:"last_name,omitempty"`
	Email          string `json:"email,omitempty"`
	AutoCollection string `json:"auto_collection"`
	CreatedAt      int64  `json:"created_at"`
}

// ChargebeeSubscription represents a subscription in the webhook content
type ChargebeeSubscription struct {
	ID           string `json:"id"`
	CustomerID   string `json:"customer_id"`
	CurrencyCode string `json:"currency_code"`
	Status       string `json:"status"`
	CreatedAt    int64  `json:"created_at"`
}

// ChargebeeEventType represents the types of events we handle
type ChargebeeEventType string

const (
	EventPaymentSucceeded ChargebeeEventType = "payment_succeeded"
	EventPaymentFailed    ChargebeeEventType = "payment_failed"
	EventInvoiceGenerated ChargebeeEventType = "invoice_generated"
	EventInvoiceUpdated   ChargebeeEventType = "invoice_updated"
)

// timestampToTime converts a Unix timestamp (int64) to time.Time
func timestampToTime(timestamp int64) time.Time {
	return time.Unix(timestamp, 0).UTC()
}
