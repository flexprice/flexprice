package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
)

type InternalWalletEvent struct {
	EventType string `json:"event_type"`
	WalletID  string `json:"wallet_id"`
	TenantID  string `json:"tenant_id"`
}

// SubscriptionWebhookPayload represents the detailed payload for subscription payment webhooks
type WalletWebhookPayload struct {
	Wallet *dto.WalletResponse
}

func NewWalletWebhookPayload(wallet *dto.WalletResponse) *WalletWebhookPayload {
	return &WalletWebhookPayload{
		Wallet: wallet,
	}
}
