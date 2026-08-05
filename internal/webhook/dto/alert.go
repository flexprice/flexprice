package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// InternalAlertEvent is what LogAlert publishes for any alert type; AlertPayloadBuilder branches
// on which fields are populated to decide how to resolve it into a webhook payload.
type InternalAlertEvent struct {
	FeatureID   string           `json:"feature_id,omitempty"`
	WalletID    string           `json:"wallet_id,omitempty"`
	CustomerID  string           `json:"customer_id,omitempty"`
	AlertType   types.AlertType  `json:"alert_type"`
	AlertStatus types.AlertState `json:"alert_status"`

	// Populated only for a subscription/line-item/group spend alert (alert_settings table); empty
	// for the feature/wallet-balance alert above. EntityID is the subscription itself for a
	// subscription-level alert, or the line item/group for the other two scopes, in which case
	// ParentEntityID is the subscription it rolls up to.
	EntityType       types.AlertEntityType `json:"entity_type,omitempty"`
	EntityID         string                `json:"entity_id,omitempty"`
	ParentEntityID   string                `json:"parent_entity_id,omitempty"`
	ParentEntityType types.AlertEntityType `json:"parent_entity_type,omitempty"`
	AlertInfo        types.AlertInfo       `json:"alert_info,omitempty"`
}

// AlertWebhookPayload is the minimal webhook representation of a feature/wallet-balance alert.
// CurrentBalance/CreditBalance are kept as scalars (not an embedded Wallet object) because
// they're the actual business event data this alert exists to report.
type AlertWebhookPayload struct {
	EventType      types.WebhookEventName `json:"event_type"`
	AlertType      types.AlertType        `json:"alert_type"`
	AlertStatus    types.AlertState       `json:"alert_status"`
	FeatureID      string                 `json:"feature_id,omitempty"`
	WalletID       string                 `json:"wallet_id,omitempty"`
	CustomerID     string                 `json:"customer_id,omitempty"`
	CurrentBalance string                 `json:"current_balance,omitempty"`
	CreditBalance  string                 `json:"credit_balance,omitempty"`
}

func NewAlertWebhookPayload(feature *dto.FeatureResponse, wallet *dto.WalletResponse, customer *dto.CustomerResponse, alertType types.AlertType, alertStatus types.AlertState, eventType types.WebhookEventName) *AlertWebhookPayload {
	payload := &AlertWebhookPayload{
		EventType:   eventType,
		AlertType:   alertType,
		AlertStatus: alertStatus,
	}
	if feature != nil && feature.Feature != nil {
		payload.FeatureID = feature.ID
	}
	if wallet != nil && wallet.Wallet != nil {
		payload.WalletID = wallet.ID
		payload.CurrentBalance = wallet.Balance.String()
		payload.CreditBalance = wallet.CreditBalance.String()
	}
	if customer != nil && customer.Customer != nil {
		payload.CustomerID = customer.ID
	}
	return payload
}

// SpendAlertEvent is the webhook payload for the three alert_settings spend alert types
// (subscription, subscription line item, group).
type SpendAlertEvent struct {
	Subscription           *Subscription    `json:"subscription"`
	SubscriptionLineItemID string           `json:"subscription_line_item_id,omitempty"`
	GroupID                string           `json:"group_id,omitempty"`
	AlertType              types.AlertType  `json:"alert_type"`
	AlertStatus            types.AlertState `json:"alert_status"`
	CurrentSpend           string           `json:"current_spend"`
	TriggeredAt            time.Time        `json:"triggered_at"`
}

// NewSpendAlertEvent takes the already-fetched subscription and the raw entity/parent IDs from
// the internal alert event — no separate line-item/group fetch is needed since the payload
// only needs their IDs, which the internal event already carries.
func NewSpendAlertEvent(sub *dto.SubscriptionResponse, lineItemID, groupID string, alertType types.AlertType, alertStatus types.AlertState, currentSpend string, triggeredAt time.Time) *SpendAlertEvent {
	return &SpendAlertEvent{
		Subscription:           NewSubscription(sub),
		SubscriptionLineItemID: lineItemID,
		GroupID:                groupID,
		AlertType:              alertType,
		AlertStatus:            alertStatus,
		CurrentSpend:           currentSpend,
		TriggeredAt:            triggeredAt,
	}
}

// EntitlementGrantAlertEvent is the webhook payload for entitlement grant exhaustion.
type EntitlementGrantAlertEvent struct {
	SubscriptionID     string           `json:"subscription_id"`
	CustomerID         string           `json:"customer_id"`
	EntitlementID      string           `json:"entitlement_id"`
	EntitlementGrantID string           `json:"entitlement_grant_id"`
	AlertType          types.AlertType  `json:"alert_type"`
	AlertStatus        types.AlertState `json:"alert_status"`
	UsageRatio         string           `json:"usage_ratio"`
	TriggeredAt        time.Time        `json:"triggered_at"`
}

// NewEntitlementGrantAlertEvent takes raw IDs directly — every value here is already known
// from the internal alert event and the fetched grant, with no extra service calls needed.
func NewEntitlementGrantAlertEvent(subscriptionID, customerID, entitlementID, grantID string, alertType types.AlertType, alertStatus types.AlertState, usageRatio string, triggeredAt time.Time) *EntitlementGrantAlertEvent {
	return &EntitlementGrantAlertEvent{
		SubscriptionID:     subscriptionID,
		CustomerID:         customerID,
		EntitlementID:      entitlementID,
		EntitlementGrantID: grantID,
		AlertType:          alertType,
		AlertStatus:        alertStatus,
		UsageRatio:         usageRatio,
		TriggeredAt:        triggeredAt,
	}
}
