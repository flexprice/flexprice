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
	EventType string                `json:"event_type"`
	Wallet    *dto.WalletResponse   `json:"wallet"`
	Customer  *dto.CustomerResponse `json:"customer,omitempty"`
	Alert     *WalletAlertInfo      `json:"alert,omitempty"`
}

// WalletAlertInfo contains details about the wallet alert
type WalletAlertInfo struct {
	State          string               `json:"state"`
	CurrentBalance decimal.Decimal      `json:"current_balance"`
	CreditBalance  decimal.Decimal      `json:"credit_balance"`
	AlertType      string               `json:"alert_type,omitempty"`
	AlertSettings  *types.AlertSettings `json:"alert_settings,omitempty"`
}

type TransactionWebhookPayload struct {
	EventType   string                         `json:"event_type"`
	Transaction *dto.WalletTransactionResponse `json:"transaction"`
	Wallet      *dto.WalletResponse            `json:"wallet"`
}

func NewWalletWebhookPayload(wallet *dto.WalletResponse, customer *dto.CustomerResponse, alert *WalletAlertInfo, eventType string) *WalletWebhookPayload {
	return &WalletWebhookPayload{
		EventType: eventType,
		Wallet:    wallet,
		Customer:  customer,
		Alert:     alert,
	}
}

func NewTransactionWebhookPayload(transaction *dto.WalletTransactionResponse, wallet *dto.WalletResponse, eventType string) *TransactionWebhookPayload {
	return &TransactionWebhookPayload{
		EventType:   eventType,
		Transaction: transaction,
		Wallet:      wallet,
	}
}
