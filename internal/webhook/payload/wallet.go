package payload

import (
	"context"
	"encoding/json"

	ierr "github.com/flexprice/flexprice/internal/errors"
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

func (b WalletPayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	// Validate input data
	var parsedPayload webhookDto.InternalWalletEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal wallet event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	// Create payload
	walletData, err := b.services.WalletService.GetWalletByID(ctx, parsedPayload.WalletID)
	if err != nil {
		return nil, err
	}

	// Create webhook payload with alert info if present
	payload := webhookDto.NewWalletWebhookPayload(walletData, parsedPayload.Alert)

	// Marshal payload
	return json.Marshal(payload)
}

func (b TransactionPayloadBuilder) BuildPayload(
	ctx context.Context,
	eventType string,
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

	payload := webhookDto.NewTransactionWebhookPayload(transactionData, walletData)

	return json.Marshal(payload)

}
