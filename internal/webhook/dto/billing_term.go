package webhookDto

import (
	"github.com/flexprice/flexprice/internal/domain/billingterm"
	"github.com/flexprice/flexprice/internal/types"
)

// InternalBillingTermsEvent is the stored event payload; the builder reads the owner's terms when it sends.
type InternalBillingTermsEvent struct {
	OwnerType types.BillingTermOwnerType `json:"owner_type"`
	OwnerID   string                     `json:"owner_id"`
	TenantID  string                     `json:"tenant_id"`
}

// InternalBillingInstalmentEvent is the stored event payload; the builder reads the instalment when it sends.
type InternalBillingInstalmentEvent struct {
	InstalmentID string `json:"instalment_id"`
	TenantID     string `json:"tenant_id"`
}

// BillingTermsWebhookPayload is the body of billing_terms.* webhooks: the owner's current terms.
type BillingTermsWebhookPayload struct {
	EventType    types.WebhookEventName     `json:"event_type"`
	OwnerType    types.BillingTermOwnerType `json:"owner_type"`
	OwnerID      string                     `json:"owner_id"`
	BillingTerms []*billingterm.Term        `json:"billing_terms"`
}

// NewBillingTermsWebhookPayload builds the body of a billing_terms.* webhook.
func NewBillingTermsWebhookPayload(
	ownerType types.BillingTermOwnerType,
	ownerID string,
	terms []*billingterm.Term,
	eventType types.WebhookEventName,
) *BillingTermsWebhookPayload {
	return &BillingTermsWebhookPayload{
		EventType:    eventType,
		OwnerType:    ownerType,
		OwnerID:      ownerID,
		BillingTerms: terms,
	}
}

// BillingInstalmentWebhookPayload is the body of billing_instalment.* webhooks: the instalment as stored.
type BillingInstalmentWebhookPayload struct {
	EventType  types.WebhookEventName  `json:"event_type"`
	Instalment *billingterm.Instalment `json:"instalment"`
}

// NewBillingInstalmentWebhookPayload builds the body of a billing_instalment.* webhook.
func NewBillingInstalmentWebhookPayload(instalment *billingterm.Instalment, eventType types.WebhookEventName) *BillingInstalmentWebhookPayload {
	return &BillingInstalmentWebhookPayload{EventType: eventType, Instalment: instalment}
}
