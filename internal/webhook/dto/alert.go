package webhookDto

import "github.com/flexprice/flexprice/internal/api/dto"

type InternalAlertEvent struct {
	FeatureID   string `json:"feature_id,omitempty"`
	WalletID    string `json:"wallet_id,omitempty"`
	AlertType   string `json:"alert_type"`
	AlertStatus string `json:"alert_status"`
}

type AlertWebhookPayload struct {
	EventType   string               `json:"event_type"`
	AlertType   string               `json:"alert_type"`
	AlertStatus string               `json:"alert_status"`
	Feature     *dto.FeatureResponse `json:"feature,omitempty"`
	Wallet      *dto.WalletResponse  `json:"wallet,omitempty"`
}

func NewAlertWebhookPayload(feature *dto.FeatureResponse, wallet *dto.WalletResponse, alertType string, alertStatus string, eventType string) *AlertWebhookPayload {
	return &AlertWebhookPayload{EventType: eventType, AlertType: alertType, AlertStatus: alertStatus, Feature: feature, Wallet: wallet}
}
