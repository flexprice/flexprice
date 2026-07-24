package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/domain/subscription"
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

// SpendAlertEvent is the webhook payload for the three alert_settings spend alert types
// (subscription, subscription line item, group). Subscription/AlertSettings are always set;
// SubscriptionLineItem/Group only for their scope. SubscriptionLineItem is the plain domain
// object, not the dto wrapper, whose Price field pulls in the same Plan bloat stripped below.
type SpendAlertEvent struct {
	Subscription         *dto.SubscriptionResponse          `json:"subscription"`
	SubscriptionLineItem *subscription.SubscriptionLineItem `json:"subscription_line_item,omitempty"`
	Group                *dto.GroupResponse                 `json:"group,omitempty"`
	AlertType            types.AlertType                    `json:"alert_type"`
	AlertStatus          types.AlertState                   `json:"alert_status"`
	AlertSettings        *types.AlertSettings               `json:"alert_settings,omitempty"`
	CurrentSpend         string                             `json:"current_spend"`
	TriggeredAt          time.Time                          `json:"triggered_at"`
}

type AlertWebhookPayload struct {
	EventType   types.WebhookEventName `json:"event_type"`
	AlertType   types.AlertType        `json:"alert_type"`
	AlertStatus types.AlertState       `json:"alert_status"`
	Feature     *dto.FeatureResponse   `json:"feature,omitempty"`
	Wallet      *dto.WalletResponse    `json:"wallet,omitempty"`
	Customer    *dto.CustomerResponse  `json:"customer,omitempty"`
}

// EntitlementGrantAlertEvent is the webhook payload for entitlement grant exhaustion.
// UsageRatio is usage/quota at evaluation time (>= 1 when exhausted).
type EntitlementGrantAlertEvent struct {
	Subscription       *dto.SubscriptionResponse `json:"subscription"`
	EntitlementGrantID string                    `json:"entitlement_grant_id"`
	AlertType          types.AlertType           `json:"alert_type"`
	AlertStatus        types.AlertState          `json:"alert_status"`
	UsageRatio         string                    `json:"usage_ratio"`
	TriggeredAt        time.Time                 `json:"triggered_at"`
}

func NewAlertWebhookPayload(feature *dto.FeatureResponse, wallet *dto.WalletResponse, customer *dto.CustomerResponse, alertType types.AlertType, alertStatus types.AlertState, eventType types.WebhookEventName) *AlertWebhookPayload {
	return &AlertWebhookPayload{EventType: eventType, AlertType: alertType, AlertStatus: alertStatus, Feature: feature, Wallet: wallet, Customer: customer}
}
