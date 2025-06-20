package types

import (
	"encoding/json"
	"time"
)

// WebhookEvent represents a webhook event to be delivered
type WebhookEvent struct {
	ID            string          `json:"id"`
	EventName     string          `json:"event_name"`
	TenantID      string          `json:"tenant_id"`
	EnvironmentID string          `json:"environment_id"`
	UserID        string          `json:"user_id"`
	Timestamp     time.Time       `json:"timestamp"`
	Payload       json.RawMessage `json:"payload"`
}

// Common webhook event names
const (
	WebhookEventInvoiceCreateDraft     = "invoice.create.drafted"
	WebhookEventInvoiceUpdateFinalized = "invoice.update.finalized"
	WebhookEventInvoiceUpdatePayment   = "invoice.updated.payment"
	WebhookEventInvoiceUpdateVoided    = "invoice.update.voided"
)

// subscription event names
const (
	WebhookEventSubscriptionCreated   = "subscription.created"
	WebhookEventSubscriptionPaused    = "subscription.paused"
	WebhookEventSubscriptionCancelled = "subscription.cancelled"
	WebhookEventSubscriptionResumed   = "subscription.resumed"
	WebhookEventSubscriptionExpired   = "subscription.expired"
)

// wallet event names
const (
	WebhookEventWalletCreated                   = "wallet.created"
	WebhookEventWalletUpdated                   = "wallet.updated"
	WebhookEventWalletTerminated                = "wallet.terminated"
	WebhookEventWalletDepletedOngoingBalance    = "wallet.depleted_ongoing_balance"
	WebhookEventWalletTransactionCreated        = "wallet.transaction.created"
	WebhookEventWalletTransactionUpdated        = "wallet.transaction.updated"
	WebhookEventWalletTransactionPaymentFailure = "wallet.transaction.payment_failure"
	WebhookEventWalletTransactionPaymentSuccess = "wallet.transaction.payment_success"
	WebhookEventWalletTransactionPaymentFailed  = "wallet.transaction.payment_failed"
)

// payment event names
const (
	WebhookEventPaymentCreated  = "payment.created"
	WebhookEventPaymentUpdated  = "payment.updated"
	WebhookEventPaymentFailed   = "payment.failed"
	WebhookEventPaymentSuccess  = "payment.success"
	WebhookEventPaymentPending  = "payment.pending"
	WebhookEventPaymentDeclined = "payment.declined"
	WebhookEventPaymentReversed = "payment.reversed"
)

// customer event names
const (
	WebhookEventCustomerCreated = "customer.created"
	WebhookEventCustomerUpdated = "customer.updated"
	WebhookEventCustomerDeleted = "customer.deleted"
)
