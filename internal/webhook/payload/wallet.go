package payload

import (
	"context"
	"encoding/json"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/shopspring/decimal"
)

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
	EventType types.WebhookEventName      `json:"event_type"`
	Wallet    *Wallet                     `json:"wallet"`
	Alert     *webhookDto.WalletAlertInfo `json:"alert,omitempty"`
}

type TransactionWebhookPayload struct {
	EventType   types.WebhookEventName `json:"event_type"`
	Transaction *WalletTransaction     `json:"transaction"`
}

type TransactionUpdatedWebhookPayload struct {
	EventType          types.WebhookEventName `json:"event_type"`
	UpdatedTransaction *WalletTransaction     `json:"updated_transaction"`
}

func NewWalletWebhookPayload(wallet *dto.WalletResponse, alert *webhookDto.WalletAlertInfo, eventType types.WebhookEventName) *WalletWebhookPayload {
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

type WalletPayloadBuilder struct {
	services *Services
}

type TransactionPayloadBuilder struct {
	services *Services
}

func NewWalletPayloadBuilder(services *Services) PayloadBuilder {
	return WalletPayloadBuilder{
		services: services,
	}
}

func NewTransactionPayloadBuilder(services *Services) PayloadBuilder {
	return TransactionPayloadBuilder{
		services: services,
	}
}

func (b WalletPayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	// Validate input data
	var parsedPayload webhookDto.InternalWalletEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal wallet event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	var walletData *dto.WalletResponse

	switch eventType {
	case types.WebhookEventWalletOngoingBalanceUpdated:
		if parsedPayload.Balance == nil {
			return nil, ierr.NewError("missing balance in ongoing_balance.updated internal payload").
				WithHint("InternalWalletEvent.Balance is required for wallet.ongoing_balance.updated").
				Mark(ierr.ErrInvalidOperation)
		}

		walletData = dto.WalletResponseFromBalance(parsedPayload.Balance)
		if walletData == nil {
			return nil, ierr.NewError("invalid balance in ongoing_balance.updated internal payload").
				WithHint("InternalWalletEvent.Balance must include a wallet").
				Mark(ierr.ErrInvalidOperation)
		}
	default:
		walletData, err = b.services.WalletService.GetWalletByID(ctx, parsedPayload.WalletID)
		if err != nil {
			return nil, err
		}
	}

	payload := NewWalletWebhookPayload(walletData, parsedPayload.Alert, eventType)

	return json.Marshal(payload)
}

func (b TransactionPayloadBuilder) BuildPayload(
	ctx context.Context,
	eventType types.WebhookEventName,
	data json.RawMessage,
) (json.RawMessage, error) {

	var parsedPayload webhookDto.InternalTransactionEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal InternalTransactionEvent payload").
			Mark(ierr.ErrInvalidOperation)
	}

	transactionData, err := b.services.WalletService.GetWalletTransactionByID(ctx, parsedPayload.TransactionID)
	if err != nil {
		return nil, err
	}

	var payload any
	if eventType == types.WebhookEventWalletTransactionUpdated {
		payload = NewTransactionUpdatedWebhookPayload(transactionData, eventType)
	} else {
		payload = NewTransactionWebhookPayload(transactionData, eventType)
	}

	return json.Marshal(payload)

}
