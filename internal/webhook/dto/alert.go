package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
)

type InternalAlertEvent struct {
	FeatureID   string `json:"feature_id,omitempty"`
	WalletID    string `json:"wallet_id,omitempty"`
	CustomerID  string `json:"customer_id,omitempty"`
	AlertType   string `json:"alert_type"`
	AlertStatus string `json:"alert_status"`
}

type AlertWebhookPayload struct {
	EventType   types.WebhookEventName `json:"event_type"`
	AlertType   string                 `json:"alert_type"`
	AlertStatus string                 `json:"alert_status"`
	Feature     *dto.FeatureResponse   `json:"feature,omitempty"`
	Wallet      *dto.WalletResponse    `json:"wallet,omitempty"`
	Customer    *dto.CustomerResponse  `json:"customer,omitempty"`
}

func NewAlertWebhookPayload(feature *dto.FeatureResponse, wallet *dto.WalletResponse, customer *dto.CustomerResponse, alertType string, alertStatus string, eventType types.WebhookEventName) *AlertWebhookPayload {
	return &AlertWebhookPayload{EventType: eventType, AlertType: alertType, AlertStatus: alertStatus, Feature: feature, Wallet: wallet, Customer: customer}
}
