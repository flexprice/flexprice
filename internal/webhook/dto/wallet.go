package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InternalWalletEvent struct {
	EventType types.WebhookEventName     `json:"event_type"`
	WalletID  string                     `json:"wallet_id"`
	TenantID  string                     `json:"tenant_id"`
	Alert     *WalletAlertInfo           `json:"alert,omitempty"`
	Balance   *dto.WalletBalanceResponse `json:"balance,omitempty"`
}

type InternalTransactionEvent struct {
	EventType     types.WebhookEventName `json:"event_type"`
	TransactionID string                 `json:"transaction_id"`
	TenantID      string                 `json:"tenant_id"`
}

// WalletAlertInfo contains details about the wallet alert
type WalletAlertInfo struct {
	State          string               `json:"state"`
	CurrentBalance decimal.Decimal      `json:"current_balance"`
	CreditBalance  decimal.Decimal      `json:"credit_balance"`
	AlertType      string               `json:"alert_type,omitempty"`
	AlertSettings  *types.AlertSettings `json:"alert_settings,omitempty"`
}
