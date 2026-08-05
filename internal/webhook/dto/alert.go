package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InternalAlertEvent struct {
	FeatureID   string           `json:"feature_id,omitempty"`
	WalletID    string           `json:"wallet_id,omitempty"`
	CustomerID  string           `json:"customer_id,omitempty"`
	AlertType   types.AlertType  `json:"alert_type"`
	AlertStatus types.AlertState `json:"alert_status"`

	EntityType       types.AlertEntityType `json:"entity_type,omitempty"`
	EntityID         string                `json:"entity_id,omitempty"`
	ParentEntityID   string                `json:"parent_entity_id,omitempty"`
	ParentEntityType types.AlertEntityType `json:"parent_entity_type,omitempty"`
	AlertInfo        types.AlertInfo       `json:"alert_info,omitempty"`
}

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

type SpendAlertEvent struct {
	Subscription           *Subscription    `json:"subscription"`
	SubscriptionLineItemID string           `json:"subscription_line_item_id,omitempty"`
	GroupID                string           `json:"group_id,omitempty"`
	AlertType              types.AlertType  `json:"alert_type"`
	AlertStatus            types.AlertState `json:"alert_status"`
	CurrentSpend           string           `json:"current_spend"`
	Threshold              *decimal.Decimal `json:"threshold,omitempty" swaggertype:"string"`
	TriggeredAt            time.Time        `json:"triggered_at"`
}

func thresholdForAlertStatus(settings *types.AlertSettings, status types.AlertState) *decimal.Decimal {
	if settings == nil {
		return nil
	}
	var t *types.AlertThreshold
	switch status {
	case types.AlertStateInAlarm:
		t = settings.Critical
	case types.AlertStateWarning:
		t = settings.Warning
	case types.AlertStateInfo:
		t = settings.Info
	}
	if t == nil {
		return nil
	}
	return &t.Threshold
}

func NewSpendAlertEvent(sub *dto.SubscriptionResponse, lineItemID, groupID string, alertType types.AlertType, alertStatus types.AlertState, currentSpend string, alertSettings *types.AlertSettings, triggeredAt time.Time) *SpendAlertEvent {
	return &SpendAlertEvent{
		Subscription:           NewSubscription(sub),
		SubscriptionLineItemID: lineItemID,
		GroupID:                groupID,
		AlertType:              alertType,
		AlertStatus:            alertStatus,
		CurrentSpend:           currentSpend,
		Threshold:              thresholdForAlertStatus(alertSettings, alertStatus),
		TriggeredAt:            triggeredAt,
	}
}

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
