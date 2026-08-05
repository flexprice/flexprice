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
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type InvoiceLineItem struct {
	ID              string          `json:"id"`
	PriceID         *string         `json:"price_id,omitempty"`
	DisplayName     *string         `json:"display_name,omitempty"`
	PlanDisplayName *string         `json:"plan_display_name,omitempty"`
	Amount          decimal.Decimal `json:"amount" swaggertype:"string"`
	Quantity        decimal.Decimal `json:"quantity" swaggertype:"string"`
	PeriodStart     *time.Time      `json:"period_start,omitempty"`
	PeriodEnd       *time.Time      `json:"period_end,omitempty"`
}

type Invoice struct {
	ID              string              `json:"id"`
	CustomerID      string              `json:"customer_id"`
	SubscriptionID  *string             `json:"subscription_id,omitempty"`
	InvoiceType     types.InvoiceType   `json:"invoice_type"`
	InvoiceStatus   types.InvoiceStatus `json:"invoice_status"`
	PaymentStatus   types.PaymentStatus `json:"payment_status"`
	Currency        string              `json:"currency"`
	AmountDue       decimal.Decimal     `json:"amount_due" swaggertype:"string"`
	AmountPaid      decimal.Decimal     `json:"amount_paid" swaggertype:"string"`
	AmountRemaining decimal.Decimal     `json:"amount_remaining" swaggertype:"string"`
	Total           decimal.Decimal     `json:"total" swaggertype:"string"`
	Subtotal        decimal.Decimal     `json:"subtotal" swaggertype:"string"`
	InvoiceNumber   *string             `json:"invoice_number,omitempty"`
	DueDate         *time.Time          `json:"due_date,omitempty"`
	PaidAt          *time.Time          `json:"paid_at,omitempty"`
	VoidedAt        *time.Time          `json:"voided_at,omitempty"`
	FinalizedAt     *time.Time          `json:"finalized_at,omitempty"`
	PeriodStart     *time.Time          `json:"period_start,omitempty"`
	PeriodEnd       *time.Time          `json:"period_end,omitempty"`
	InvoicePDFURL   *string             `json:"invoice_pdf_url,omitempty"`
	BillingReason   string              `json:"billing_reason,omitempty"`
	Subscription    *Subscription       `json:"subscription,omitempty"`
	Customer        *Customer           `json:"customer,omitempty"`
	LineItems       []*InvoiceLineItem  `json:"line_items,omitempty"`
	Metadata        types.Metadata      `json:"metadata,omitempty"`
}

func newInvoiceLineItem(item *dto.InvoiceLineItemResponse) *InvoiceLineItem {
	if item == nil {
		return nil
	}
	return &InvoiceLineItem{
		ID:              item.ID,
		PriceID:         item.PriceID,
		DisplayName:     item.DisplayName,
		PlanDisplayName: item.PlanDisplayName,
		Amount:          item.Amount,
		Quantity:        item.Quantity,
		PeriodStart:     item.PeriodStart,
		PeriodEnd:       item.PeriodEnd,
	}
}

// NewInvoice builds the minimal payload. Line items are included in full only for
// invoice.update.finalized -- the one event where a consumer actually needs the itemized
// breakdown -- matching the original root-cause finding that line items were the dominant
// driver of the >3MB payloads that motivated this rework. The embedded customer object is
// also finalized-only, added back after the Svix subscriber report showed 78 active
// subscribers on this event -- higher than any other single event type.
func NewInvoice(resp *dto.InvoiceResponse, eventType types.WebhookEventName) *Invoice {
	if resp == nil {
		return nil
	}

	var lineItems []*InvoiceLineItem
	if eventType == types.WebhookEventInvoiceUpdateFinalized {
		lineItems = make([]*InvoiceLineItem, 0, len(resp.LineItems))
		for _, item := range resp.LineItems {
			if li := newInvoiceLineItem(item); li != nil {
				lineItems = append(lineItems, li)
			}
		}
	}

	var customer *Customer
	if eventType == types.WebhookEventInvoiceUpdateFinalized {
		customer = NewCustomer(resp.Customer)
	}

	return &Invoice{
		ID:              resp.ID,
		CustomerID:      resp.CustomerID,
		SubscriptionID:  resp.SubscriptionID,
		InvoiceType:     resp.InvoiceType,
		InvoiceStatus:   resp.InvoiceStatus,
		PaymentStatus:   resp.PaymentStatus,
		Currency:        resp.Currency,
		AmountDue:       resp.AmountDue,
		AmountPaid:      resp.AmountPaid,
		AmountRemaining: resp.AmountRemaining,
		Total:           resp.Total,
		Subtotal:        resp.Subtotal,
		InvoiceNumber:   resp.InvoiceNumber,
		DueDate:         resp.DueDate,
		PaidAt:          resp.PaidAt,
		VoidedAt:        resp.VoidedAt,
		FinalizedAt:     resp.FinalizedAt,
		PeriodStart:     resp.PeriodStart,
		PeriodEnd:       resp.PeriodEnd,
		InvoicePDFURL:   resp.InvoicePDFURL,
		BillingReason:   resp.BillingReason,
		Subscription:    NewSubscription(resp.Subscription),
		Customer:        customer,
		LineItems:       lineItems,
		Metadata:        resp.Metadata,
	}
}

type InvoiceWebhookPayload struct {
	EventType types.WebhookEventName `json:"event_type"`
	Invoice   *Invoice               `json:"invoice"`
}

func NewInvoiceWebhookPayload(invoice *dto.InvoiceResponse, eventType types.WebhookEventName) *InvoiceWebhookPayload {
	return &InvoiceWebhookPayload{EventType: eventType, Invoice: NewInvoice(invoice, eventType)}
}

type InvoicePayloadBuilder struct {
	services *Services
}

func NewInvoicePayloadBuilder(services *Services) PayloadBuilder {
	return &InvoicePayloadBuilder{
		services: services,
	}
}

// BuildPayload builds the webhook payload for invoice events
func (b *InvoicePayloadBuilder) BuildPayload(ctx context.Context, eventType types.WebhookEventName, data json.RawMessage) (json.RawMessage, error) {
	var parsedPayload webhookDto.InternalInvoiceEvent

	err := json.Unmarshal(data, &parsedPayload)
	if err != nil {
		return nil, ierr.WithError(err).
			WithHint("Unable to unmarshal invoice event payload").
			Mark(ierr.ErrInvalidOperation)
	}

	invoiceID, tenantID := parsedPayload.InvoiceID, parsedPayload.TenantID
	if invoiceID == "" || tenantID == "" {
		return nil, ierr.NewError("invalid data type for invoice event").
			WithHint("Please provide a valid invoice ID and tenant ID").
			WithReportableDetails(map[string]any{
				"expected": "string",
				"got":      fmt.Sprintf("%T", data),
			}).
			Mark(ierr.ErrInvalidOperation)
	}

	// Get invoice details
	invoice, err := b.services.InvoiceService.GetInvoice(ctx, invoiceID)
	if err != nil && !ierr.IsNotFound(err) {
		return nil, err
	}

	// TODO: this is a temporary fix to handle the invoice not found error.
	if ierr.IsNotFound(err) {
		time.Sleep(15 * time.Second)
		invoice, err = b.services.InvoiceService.GetInvoice(ctx, invoiceID)
		if err != nil {
			return nil, err
		}
	}

	// inject the invoice pdf url into the invoice response
	pdfUrl, err := b.services.InvoiceService.GetInvoicePDFUrl(ctx, invoiceID, false)
	if err != nil {
		b.services.Tracing.CaptureException(ctx, err)

	}
	invoice.InvoicePDFURL = lo.ToPtr(pdfUrl)

	payload := NewInvoiceWebhookPayload(invoice, eventType)

	// Return the invoice response as is
	return json.Marshal(payload)
}
