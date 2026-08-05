package outbound

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// WalletWebhookPayload is the minimal webhook representation of a wallet.
type WalletWebhookPayload struct {
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

func NewWalletWebhookPayload(resp *dto.WalletResponse) *WalletWebhookPayload {
	if resp == nil || resp.Wallet == nil {
		return nil
	}
	return &WalletWebhookPayload{
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

// WalletTransactionWebhookPayload is the minimal webhook representation of a wallet transaction.
type WalletTransactionWebhookPayload struct {
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
}

func NewWalletTransactionWebhookPayload(resp *dto.WalletTransactionResponse) *WalletTransactionWebhookPayload {
	if resp == nil || resp.Transaction == nil {
		return nil
	}
	return &WalletTransactionWebhookPayload{
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
	}
}
