package outbound

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

// AlertWebhookPayload is the minimal webhook representation of a feature/wallet-balance alert.
// CurrentBalance/CreditBalance are kept as scalars (not an embedded Wallet object) because
// they're the actual business event data this alert exists to report.
type AlertWebhookPayload struct {
	AlertType      types.AlertType  `json:"alert_type"`
	AlertStatus    types.AlertState `json:"alert_status"`
	FeatureID      string           `json:"feature_id,omitempty"`
	WalletID       string           `json:"wallet_id,omitempty"`
	CustomerID     string           `json:"customer_id,omitempty"`
	CurrentBalance string           `json:"current_balance,omitempty"`
	CreditBalance  string           `json:"credit_balance,omitempty"`
}

func NewAlertWebhookPayload(feature *dto.FeatureResponse, wallet *dto.WalletResponse, customer *dto.CustomerResponse, alertType types.AlertType, alertStatus types.AlertState) *AlertWebhookPayload {
	payload := &AlertWebhookPayload{
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

// SpendAlertWebhookPayload is the minimal webhook representation of a subscription/line-item/group
// spend-threshold alert.
type SpendAlertWebhookPayload struct {
	Subscription           *SubscriptionWebhookPayload `json:"subscription"`
	SubscriptionLineItemID string                      `json:"subscription_line_item_id,omitempty"`
	GroupID                string                      `json:"group_id,omitempty"`
	AlertType              types.AlertType             `json:"alert_type"`
	AlertStatus            types.AlertState            `json:"alert_status"`
	CurrentSpend           string                      `json:"current_spend"`
	TriggeredAt            time.Time                   `json:"triggered_at"`
}

// NewSpendAlertWebhookPayload takes the already-fetched subscription and the raw entity/parent
// IDs from the internal alert event — no separate line-item/group fetch is needed since the
// payload only needs their IDs, which the internal event already carries.
func NewSpendAlertWebhookPayload(sub *dto.SubscriptionResponse, lineItemID, groupID string, alertType types.AlertType, alertStatus types.AlertState, currentSpend string, triggeredAt time.Time) *SpendAlertWebhookPayload {
	return &SpendAlertWebhookPayload{
		Subscription:           NewSubscriptionWebhookPayload(sub),
		SubscriptionLineItemID: lineItemID,
		GroupID:                groupID,
		AlertType:              alertType,
		AlertStatus:            alertStatus,
		CurrentSpend:           currentSpend,
		TriggeredAt:            triggeredAt,
	}
}

// EntitlementGrantAlertWebhookPayload is the minimal webhook representation of an
// entitlement-grant-exhaustion alert.
type EntitlementGrantAlertWebhookPayload struct {
	SubscriptionID     string           `json:"subscription_id"`
	CustomerID         string           `json:"customer_id"`
	EntitlementID      string           `json:"entitlement_id"`
	EntitlementGrantID string           `json:"entitlement_grant_id"`
	AlertType          types.AlertType  `json:"alert_type"`
	AlertStatus        types.AlertState `json:"alert_status"`
	UsageRatio         string           `json:"usage_ratio"`
	TriggeredAt        time.Time        `json:"triggered_at"`
}

// NewEntitlementGrantAlertWebhookPayload takes raw IDs directly — every value here is already
// known from the internal alert event and the fetched grant, with no extra service calls needed.
func NewEntitlementGrantAlertWebhookPayload(subscriptionID, customerID, entitlementID, grantID string, alertType types.AlertType, alertStatus types.AlertState, usageRatio string, triggeredAt time.Time) *EntitlementGrantAlertWebhookPayload {
	return &EntitlementGrantAlertWebhookPayload{
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
