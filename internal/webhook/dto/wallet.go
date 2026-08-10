package webhookDto

import (
	"time"

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

type Wallet struct {
	ID             string             `json:"id"`
	CustomerID     string             `json:"customer_id"`
	Currency       string             `json:"currency"`
	Balance        decimal.Decimal    `json:"balance" swaggertype:"string"`
	CreditBalance  decimal.Decimal    `json:"credit_balance" swaggertype:"string"`
	WalletStatus   types.WalletStatus `json:"wallet_status"`
	Name           string             `json:"name,omitempty"`
	WalletType     types.WalletType   `json:"wallet_type"`
	ConversionRate decimal.Decimal    `json:"conversion_rate" swaggertype:"string"`
}

func NewWallet(resp *dto.WalletResponse) *Wallet {
	if resp == nil || resp.Wallet == nil {
		return nil
	}
	return &Wallet{
		ID:             resp.ID,
		CustomerID:     resp.CustomerID,
		Currency:       resp.Currency,
		Balance:        resp.Balance,
		CreditBalance:  resp.CreditBalance,
		WalletStatus:   resp.WalletStatus,
		Name:           resp.Name,
		WalletType:     resp.WalletType,
		ConversionRate: resp.ConversionRate,
	}
}

type WalletTransaction struct {
	ID                  string                      `json:"id"`
	WalletID            string                      `json:"wallet_id"`
	CustomerID          string                      `json:"customer_id"`
	Type                types.TransactionType       `json:"type"`
	Amount              decimal.Decimal             `json:"amount" swaggertype:"string"`
	CreditAmount        decimal.Decimal             `json:"credit_amount" swaggertype:"string"`
	CreditBalanceBefore decimal.Decimal             `json:"credit_balance_before" swaggertype:"string"`
	CreditBalanceAfter  decimal.Decimal             `json:"credit_balance_after" swaggertype:"string"`
	TxStatus            types.TransactionStatus     `json:"transaction_status"`
	ReferenceType       types.WalletTxReferenceType `json:"reference_type"`
	ReferenceID         string                      `json:"reference_id,omitempty"`
	Description         string                      `json:"description,omitempty"`
	ExpiryDate          *time.Time                  `json:"expiry_date,omitempty"`
	TransactionReason   types.TransactionReason     `json:"transaction_reason"`
	Currency            string                      `json:"currency"`
	Metadata            types.Metadata              `json:"metadata,omitempty"`
}

func NewWalletTransaction(resp *dto.WalletTransactionResponse) *WalletTransaction {
	if resp == nil || resp.Transaction == nil {
		return nil
	}
	return &WalletTransaction{
		ID:                  resp.ID,
		WalletID:            resp.WalletID,
		CustomerID:          resp.CustomerID,
		Type:                resp.Type,
		Amount:              resp.Amount,
		CreditAmount:        resp.CreditAmount,
		CreditBalanceBefore: resp.CreditBalanceBefore,
		CreditBalanceAfter:  resp.CreditBalanceAfter,
		TxStatus:            resp.TxStatus,
		ReferenceType:       resp.ReferenceType,
		ReferenceID:         resp.ReferenceID,
		Description:         resp.Description,
		ExpiryDate:          resp.ExpiryDate,
		TransactionReason:   resp.TransactionReason,
		Currency:            resp.Currency,
		Metadata:            resp.Metadata,
	}
}

// WalletWebhookPayload represents the detailed payload for wallet webhooks
type WalletWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Wallet    *Wallet                `json:"wallet"`
	Alert     *WalletAlertInfo       `json:"alert,omitempty"`
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
	EventType   types.WebhookEventName `json:"event_type"`
	Transaction *WalletTransaction     `json:"transaction"`
}

type TransactionUpdatedWebhookPayload struct {
	EventType          types.WebhookEventName `json:"event_type"`
	UpdatedTransaction *WalletTransaction     `json:"updated_transaction"`
}

func NewWalletWebhookPayload(wallet *dto.WalletResponse, alert *WalletAlertInfo, eventType types.WebhookEventName) *WalletWebhookPayload {
	return &WalletWebhookPayload{
		EventType: eventType,
		Wallet:    NewWallet(wallet),
		Alert:     alert,
	}
}

func NewTransactionWebhookPayload(transaction *dto.WalletTransactionResponse, eventType types.WebhookEventName) *TransactionWebhookPayload {
	return &TransactionWebhookPayload{
		EventType:   eventType,
		Transaction: NewWalletTransaction(transaction),
	}
}

func NewTransactionUpdatedWebhookPayload(transaction *dto.WalletTransactionResponse, eventType types.WebhookEventName) *TransactionUpdatedWebhookPayload {
	return &TransactionUpdatedWebhookPayload{
		EventType:          eventType,
		UpdatedTransaction: NewWalletTransaction(transaction),
	}
}
