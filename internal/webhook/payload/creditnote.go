package payload

import (
	"context"
	"encoding/json"
	"fmt"

	ierr "github.com/flexprice/flexprice/internal/errors"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
)

type CreditNotePayloadBuilder struct {
	services *Services
}

func NewCreditNotePayloadBuilder(services *Services) PayloadBuilder {
	return &CreditNotePayloadBuilder{
		services: services,
	}
}

func (b *CreditNotePayloadBuilder) BuildPayload(ctx context.Context, eventType string, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalCreditNoteEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal credit note event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	creditNoteID, tenantID := parsedPayload.CreditNoteID, parsedPayload.TenantID

	if creditNoteID == "" || tenantID == "" {
		return nil, ierr.NewError("invalid data type for credit note event").
			WithHint("Please provide a valid credit note ID and tenant ID").
			WithReportableDetails(map[string]any{
				"expected": "string",
				"got":      fmt.Sprintf("%T", data),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// get credit note details
	creditNote, err := b.services.CreditNoteService.GetCreditNote(ctx, creditNoteID)
	if err != nil {
		return nil, err
	}

	payload := webhookDto.NewCreditNoteWebhookPayload(creditNote, eventType)
	return json.Marshal(payload)
}
