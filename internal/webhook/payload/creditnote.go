package payload

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	webhookDto "github.com/flexprice/flexprice/internal/webhook/dto"
	"github.com/shopspring/decimal"
)

type CreditNote struct {
	ID               string                 `json:"id"`
	CreditNoteNumber string                 `json:"credit_note_number,omitempty"`
	InvoiceID        string                 `json:"invoice_id"`
	CustomerID       string                 `json:"customer_id"`
	SubscriptionID   *string                `json:"subscription_id,omitempty"`
	CreditNoteStatus types.CreditNoteStatus `json:"credit_note_status"`
	CreditNoteType   types.CreditNoteType   `json:"credit_note_type"`
	Reason           types.CreditNoteReason `json:"reason"`
	Memo             string                 `json:"memo,omitempty"`
	Currency         string                 `json:"currency"`
	TotalAmount      decimal.Decimal        `json:"total_amount" swaggertype:"string"`
	VoidedAt         *time.Time             `json:"voided_at,omitempty"`
	FinalizedAt      *time.Time             `json:"finalized_at,omitempty"`
	Metadata         types.Metadata         `json:"metadata,omitempty"`
}

func NewCreditNote(resp *dto.CreditNoteResponse) *CreditNote {
	if resp == nil || resp.CreditNote == nil {
		return nil
	}
	return &CreditNote{
		ID:               resp.ID,
		CreditNoteNumber: resp.CreditNoteNumber,
		InvoiceID:        resp.InvoiceID,
		CustomerID:       resp.CustomerID,
		SubscriptionID:   resp.SubscriptionID,
		CreditNoteStatus: resp.CreditNoteStatus,
		CreditNoteType:   resp.CreditNoteType,
		Reason:           resp.Reason,
		Memo:             resp.Memo,
		Currency:         resp.Currency,
		TotalAmount:      resp.TotalAmount,
		VoidedAt:         resp.VoidedAt,
		FinalizedAt:      resp.FinalizedAt,
		Metadata:         resp.Metadata,
	}
}

type CreditNoteWebhookPayload struct {
	EventType  types.WebhookEventName `json:"event_type"`
	CreditNote *CreditNote            `json:"credit_note"`
}

func NewCreditNoteWebhookPayload(creditNote *dto.CreditNoteResponse, eventType types.WebhookEventName) *CreditNoteWebhookPayload {
	return &CreditNoteWebhookPayload{EventType: eventType, CreditNote: NewCreditNote(creditNote)}
}

type CreditNotePayloadBuilder struct {
	services *Services
}

func NewCreditNotePayloadBuilder(services *Services) PayloadBuilder {
	return &CreditNotePayloadBuilder{
		services: services,
	}
}

func (b *CreditNotePayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
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

	payload := NewCreditNoteWebhookPayload(creditNote, eventType)
	return json.Marshal(payload)
}
