package webhookDto

import (
	"time"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

type InternalInvoiceEvent struct {
	InvoiceID string `json:"invoice_id"`
	TenantID  string `json:"tenant_id"`
}

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
