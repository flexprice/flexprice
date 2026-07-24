package types

import (
	"context"
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
	// DerivationDepth is a runtime backstop against derivation loops: it counts how many
	// times this event was produced by deriving from another event. 0 for events emitted
	// directly by services. The webhook consumer refuses to derive past a small max depth.
	DerivationDepth int `json:"derivation_depth,omitempty"`
}

// WebhookEventBuilder centralises the WebhookEvent envelope boilerplate (ID, timestamp,
// tenant/environment/user identity, payload marshalling) that would otherwise be duplicated
// across every service's publishSystemEvent. It is entity-agnostic: callers supply the event
// name, entity reference, and a payload struct to be JSON-marshalled.
type WebhookEventBuilder struct {
	event   *WebhookEvent
	payload any
}

// NewWebhookEvent starts a builder for the given event name with a fresh system-event ID and
// current timestamp.
func NewWebhookEvent(eventName WebhookEventName) *WebhookEventBuilder {
	return &WebhookEventBuilder{
		event: &WebhookEvent{
			ID:        GenerateUUIDWithPrefix(UUID_PREFIX_SYSTEM_EVENT),
			EventName: eventName,
			Timestamp: time.Now().UTC(),
		},
	}
}

// WithIdentityFromContext copies tenant/environment/user identity from the request context.
func (b *WebhookEventBuilder) WithIdentityFromContext(ctx context.Context) *WebhookEventBuilder {
	b.event.TenantID = GetTenantID(ctx)
	b.event.EnvironmentID = GetEnvironmentID(ctx)
	b.event.UserID = GetUserID(ctx)
	return b
}

// WithIdentityFrom copies tenant/environment/user identity from a source event. Used when one
// event is derived from another. It does not touch DerivationDepth — the deriver stamps that.
func (b *WebhookEventBuilder) WithIdentityFrom(source *WebhookEvent) *WebhookEventBuilder {
	if source != nil {
		b.event.TenantID = source.TenantID
		b.event.EnvironmentID = source.EnvironmentID
		b.event.UserID = source.UserID
	}
	return b
}

// WithEntity sets the entity type/id the event refers to.
func (b *WebhookEventBuilder) WithEntity(entityType SystemEntityType, entityID string) *WebhookEventBuilder {
	b.event.EntityType = entityType
	b.event.EntityID = entityID
	return b
}

// WithPayload stores a payload struct to be JSON-marshalled at Build time.
func (b *WebhookEventBuilder) WithPayload(payload any) *WebhookEventBuilder {
	b.payload = payload
	return b
}

// Build marshals the payload (if any) and returns the assembled event.
func (b *WebhookEventBuilder) Build() (*WebhookEvent, error) {
	if b.payload != nil {
		raw, err := json.Marshal(b.payload)
		if err != nil {
			return nil, err
		}
		b.event.Payload = raw
	}
	return b.event, nil
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
	WebhookEventWalletTransactionUpdated WebhookEventName = "wallet.transaction.updated"
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
	WebhookEventWalletOngoingBalanceUpdated   WebhookEventName = "wallet.ongoing_balance.updated"

	// subscription/line-item/group spend alert events (alert_settings table, Parts A/B/C).
	WebhookEventSubscriptionSpendThresholdReached           WebhookEventName = "subscription.spend.threshold_reached"
	WebhookEventSubscriptionSpendThresholdRecovered         WebhookEventName = "subscription.spend.threshold_recovered"
	WebhookEventSubscriptionLineItemSpendThresholdReached   WebhookEventName = "subscription.line_item_spend.threshold_reached"
	WebhookEventSubscriptionLineItemSpendThresholdRecovered WebhookEventName = "subscription.line_item_spend.threshold_recovered"
	WebhookEventSubscriptionGroupSpendThresholdReached      WebhookEventName = "subscription.group_spend.threshold_reached"
	WebhookEventSubscriptionGroupSpendThresholdRecovered    WebhookEventName = "subscription.group_spend.threshold_recovered"

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

// checkout session event names
const (
	WebhookEventCheckoutSessionInitiated WebhookEventName = "checkout.session.initiated"
	WebhookEventCheckoutSessionCompleted WebhookEventName = "checkout.session.completed"
	WebhookEventCheckoutSessionFailed    WebhookEventName = "checkout.session.failed"
	WebhookEventCheckoutSessionExpired   WebhookEventName = "checkout.session.expired"
)

// event (usage ingestion) webhook event names
const (
	// WebhookEventEventRejected fires when an ingested event produces no meter usage.
	WebhookEventEventRejected WebhookEventName = "event.rejected"
)

// RejectedEventReason explains why an event matched no meter.
type RejectedEventReason = string

const (
	// no meter registered for the event name
	RejectedEventReasonNoMeterForName RejectedEventReason = "no_meter_for_event_name"
	// meters exist for the name but none matched (filters/quantity)
	RejectedEventReasonNoMatchingMeter RejectedEventReason = "no_matching_meter"
)
