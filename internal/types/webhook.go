package types

import (
	"encoding/json"
	"time"
)

// WebhookEvent represents a webhook event to be delivered
type WebhookEvent struct {
	ID        string          `json:"id"`
	EventName string          `json:"event_name"`
	TenantID  string          `json:"tenant_id"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// Common webhook event names
const (
	WebhookEventInvoiceCreateDraft     = "invoice.create.drafted"
	WebhookEventInvoiceUpdateFinalized = "invoice.update.finalized"
	WebhookEventInvoiceUpdatePayment   = "invoice.updated.payment"
	WebhookEventInvoiceUpdateVoided    = "invoice.update.voided"
)
