package webhookDto

import (
	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InternalWalletEvent struct {
	EventType string                     `json:"event_type"`
	WalletID  string                     `json:"wallet_id"`
	TenantID  string                     `json:"tenant_id"`
	Alert     *WalletAlertInfo           `json:"alert,omitempty"`
	Balance   *dto.WalletBalanceResponse `json:"balance,omitempty"`
}

type InternalTransactionEvent struct {
	EventType     string `json:"event_type"`
	TransactionID string `json:"transaction_id"`
	TenantID      string `json:"tenant_id"`
}

// WalletWebhookPayload represents the detailed payload for wallet webhooks
type WalletWebhookPayload struct {
	Wallet *dto.WalletResponse `json:"wallet"`
	Alert  *WalletAlertInfo    `json:"alert,omitempty"`
}

// WalletAlertInfo contains details about the wallet alert
type WalletAlertInfo struct {
	State          string             `json:"state"`
	Threshold      decimal.Decimal    `json:"threshold"`
	CurrentBalance decimal.Decimal    `json:"current_balance"`
	CreditBalance  decimal.Decimal    `json:"credit_balance"`
	AlertType      string             `json:"alert_type,omitempty"`
	AlertConfig    *types.AlertConfig `json:"alert_config,omitempty"`
}

type TransactionWebhookPayload struct {
	Transaction *dto.WalletTransactionResponse `json:"transaction"`
	Wallet      *dto.WalletResponse            `json:"wallet"`
}

func NewWalletWebhookPayload(wallet *dto.WalletResponse, alert *WalletAlertInfo) *WalletWebhookPayload {
	return &WalletWebhookPayload{
		Wallet: wallet,
		Alert:  alert,
	}
}

func NewTransactionWebhookPayload(transaction *dto.WalletTransactionResponse, wallet *dto.WalletResponse) *TransactionWebhookPayload {
	return &TransactionWebhookPayload{
		Transaction: transaction,
		Wallet:      wallet,
	}
}
