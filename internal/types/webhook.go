package types

import (
	"encoding/json"
	"time"
)

// WebhookEventName represents a webhook event name string.
// @name WebhookEventName
type WebhookEventName = string

// WebhookEvent represents a webhook event to be delivered
type WebhookEvent struct {
	ID            string           `json:"id"`
	EventName     WebhookEventName `json:"event_name"`
	TenantID      string           `json:"tenant_id"`
	EnvironmentID string           `json:"environment_id"`
	UserID        string           `json:"user_id"`
	Timestamp     time.Time        `json:"timestamp"`
	Payload       json.RawMessage  `json:"payload"`
	EntityType    SystemEntityType `json:"entity_type,omitempty"`
	EntityID      string           `json:"entity_id,omitempty"`
}

// subscription event names
const (
	WebhookEventSubscriptionCreated      WebhookEventName = "subscription.created"
	WebhookEventSubscriptionDraftCreated WebhookEventName = "subscription.draft.created"
	WebhookEventSubscriptionActivated    WebhookEventName = "subscription.activated"
	WebhookEventSubscriptionUpdated      WebhookEventName = "subscription.updated"
	WebhookEventSubscriptionPaused       WebhookEventName = "subscription.paused"
	WebhookEventSubscriptionCancelled    WebhookEventName = "subscription.cancelled"
	WebhookEventSubscriptionResumed      WebhookEventName = "subscription.resumed"
)

// subscription phase event names
const (
	WebhookEventSubscriptionPhaseCreated WebhookEventName = "subscription.phase.created"
	WebhookEventSubscriptionPhaseUpdated WebhookEventName = "subscription.phase.updated"
	WebhookEventSubscriptionPhaseDeleted WebhookEventName = "subscription.phase.deleted"
)

// feature event names
const (
	WebhookEventFeatureCreated            WebhookEventName = "feature.created"
	WebhookEventFeatureUpdated            WebhookEventName = "feature.updated"
	WebhookEventFeatureDeleted            WebhookEventName = "feature.deleted"
	WebhookEventFeatureWalletBalanceAlert WebhookEventName = "feature.wallet_balance.alert"
)

// entitlement event names
const (
	WebhookEventEntitlementCreated WebhookEventName = "entitlement.created"
	WebhookEventEntitlementUpdated WebhookEventName = "entitlement.updated"
	WebhookEventEntitlementDeleted WebhookEventName = "entitlement.deleted"
)

// wallet event names
const (
	WebhookEventWalletCreated            WebhookEventName = "wallet.created"
	WebhookEventWalletUpdated            WebhookEventName = "wallet.updated"
	WebhookEventWalletTerminated         WebhookEventName = "wallet.terminated"
	WebhookEventWalletTransactionCreated WebhookEventName = "wallet.transaction.created"
)

// payment event names
const (
	WebhookEventPaymentCreated WebhookEventName = "payment.created"
	WebhookEventPaymentUpdated WebhookEventName = "payment.updated"
	WebhookEventPaymentFailed  WebhookEventName = "payment.failed"
	WebhookEventPaymentSuccess WebhookEventName = "payment.success"
	WebhookEventPaymentPending WebhookEventName = "payment.pending"
)

// customer event names
const (
	WebhookEventCustomerCreated WebhookEventName = "customer.created"
	WebhookEventCustomerUpdated WebhookEventName = "customer.updated"
	WebhookEventCustomerDeleted WebhookEventName = "customer.deleted"
)

// TODO: Below events should be cron triggered webhook event names
const (
	WebhookEventInvoiceUpdateFinalized WebhookEventName = "invoice.update.finalized"
	WebhookEventInvoiceUpdatePayment   WebhookEventName = "invoice.update.payment"
	WebhookEventInvoiceUpdateVoided    WebhookEventName = "invoice.update.voided"
	WebhookEventInvoiceUpdate          WebhookEventName = "invoice.update"
	WebhookEventInvoicePaymentOverdue  WebhookEventName = "invoice.payment.overdue"
)

// alert event names
const (
	WebhookEventWalletCreditBalanceDropped   WebhookEventName = "wallet.credit_balance.dropped"
	WebhookEventWalletCreditBalanceRecovered WebhookEventName = "wallet.credit_balance.recovered"

	WebhookEventWalletOngoingBalanceDropped   WebhookEventName = "wallet.ongoing_balance.dropped"
	WebhookEventWalletOngoingBalanceRecovered WebhookEventName = "wallet.ongoing_balance.recovered"

	// cron driven webhook event names
	WebhookEventSubscriptionRenewalDue WebhookEventName = "subscription.renewal.due"
)

// communication event names
const (
	WebhookEventInvoiceCommunicationTriggered WebhookEventName = "invoice.communication.triggered"
)

// credit note event names
const (
	WebhookEventCreditNoteCreated WebhookEventName = "credit_note.created"
	WebhookEventCreditNoteUpdated WebhookEventName = "credit_note.updated"
)
