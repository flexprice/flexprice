package outbound

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InvoiceLineItemWebhookPayload is the minimal webhook representation of an invoice line item.
// Only ever populated on invoice.update.finalized / invoice.update.voided — see
// NewInvoiceWebhookPayload's eventType branch below.
type InvoiceLineItemWebhookPayload struct {
	ID              string          `json:"id"`
	PriceID         *string         `json:"price_id,omitempty"`
	DisplayName     *string         `json:"display_name,omitempty"`
	PlanDisplayName *string         `json:"plan_display_name,omitempty"`
	Amount          decimal.Decimal `json:"amount" swaggertype:"string"`
	Quantity        decimal.Decimal `json:"quantity" swaggertype:"string"`
	PeriodStart     *time.Time      `json:"period_start,omitempty"`
	PeriodEnd       *time.Time      `json:"period_end,omitempty"`
}

// InvoiceWebhookPayload is the minimal webhook representation of an invoice.
type InvoiceWebhookPayload struct {
	ID              string                           `json:"id"`
	CustomerID      string                           `json:"customer_id"`
	SubscriptionID  *string                          `json:"subscription_id,omitempty"`
	InvoiceType     types.InvoiceType                `json:"invoice_type"`
	InvoiceStatus   types.InvoiceStatus              `json:"invoice_status"`
	PaymentStatus   types.PaymentStatus              `json:"payment_status"`
	Currency        string                           `json:"currency"`
	AmountDue       decimal.Decimal                  `json:"amount_due" swaggertype:"string"`
	AmountPaid      decimal.Decimal                  `json:"amount_paid" swaggertype:"string"`
	AmountRemaining decimal.Decimal                  `json:"amount_remaining" swaggertype:"string"`
	Total           decimal.Decimal                  `json:"total" swaggertype:"string"`
	Subtotal        decimal.Decimal                  `json:"subtotal" swaggertype:"string"`
	InvoiceNumber   *string                          `json:"invoice_number,omitempty"`
	DueDate         *time.Time                       `json:"due_date,omitempty"`
	PaidAt          *time.Time                       `json:"paid_at,omitempty"`
	VoidedAt        *time.Time                       `json:"voided_at,omitempty"`
	FinalizedAt     *time.Time                       `json:"finalized_at,omitempty"`
	PeriodStart     *time.Time                       `json:"period_start,omitempty"`
	PeriodEnd       *time.Time                       `json:"period_end,omitempty"`
	InvoicePDFURL   *string                          `json:"invoice_pdf_url,omitempty"`
	BillingReason   string                           `json:"billing_reason,omitempty"`
	Subscription    *SubscriptionWebhookPayload      `json:"subscription,omitempty"`
	LineItems       []*InvoiceLineItemWebhookPayload `json:"line_items,omitempty"`
}

func newInvoiceLineItemWebhookPayload(item *dto.InvoiceLineItemResponse) *InvoiceLineItemWebhookPayload {
	if item == nil {
		return nil
	}
	return &InvoiceLineItemWebhookPayload{
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

// NewInvoiceWebhookPayload builds the minimal payload. Line items are included in full only
// for invoice.update.finalized and invoice.update.voided — the two events where a consumer
// actually needs the itemized breakdown — matching the original root-cause finding that line
// items were the dominant driver of the >3MB payloads that motivated this whole rework.
func NewInvoiceWebhookPayload(resp *dto.InvoiceResponse, eventType types.WebhookEventName) *InvoiceWebhookPayload {
	if resp == nil {
		return nil
	}

	var lineItems []*InvoiceLineItemWebhookPayload
	if eventType == types.WebhookEventInvoiceUpdateFinalized || eventType == types.WebhookEventInvoiceUpdateVoided {
		lineItems = make([]*InvoiceLineItemWebhookPayload, 0, len(resp.LineItems))
		for _, item := range resp.LineItems {
			if li := newInvoiceLineItemWebhookPayload(item); li != nil {
				lineItems = append(lineItems, li)
			}
		}
	}

	return &InvoiceWebhookPayload{
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
		Subscription:    NewSubscriptionWebhookPayload(resp.Subscription),
		LineItems:       lineItems,
	}
}
