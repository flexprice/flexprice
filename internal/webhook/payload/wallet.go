package payload

import (
	"context"
	"encoding/json"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

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

	// Fetch customer data
	var customerData *dto.CustomerResponse
	if walletData.CustomerID != "" {
		customer, err := b.services.CustomerService.GetCustomer(ctx, walletData.CustomerID)
		if err != nil {
			// Log error but don't fail the webhook if customer fetch fails
			// Customer is optional in the payload
			b.services.Tracing.CaptureException(ctx, err)
			customerData = nil
		} else {
			customerData = customer
		}
	}

	// Create webhook payload with alert info and customer if present
	payload := webhookDto.NewWalletWebhookPayload(walletData, customerData, parsedPayload.Alert, eventType)

	// Marshal payload
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

	walletData, err := b.services.WalletService.GetWalletByID(ctx, transactionData.WalletID)
	if err != nil {
		return nil, err
	}

	var payload any
	if eventType == types.WebhookEventWalletTransactionUpdated {
		payload = webhookDto.NewTransactionUpdatedWebhookPayload(transactionData, walletData, eventType)
	} else {
		payload = webhookDto.NewTransactionWebhookPayload(transactionData, walletData, eventType)
	}

	return json.Marshal(payload)

}
