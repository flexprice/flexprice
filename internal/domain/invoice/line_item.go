package invoice

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InvoiceLineItem represents a single line item in an invoice
type InvoiceLineItem struct {
	ID             string          `json:"id"`
	InvoiceID      string          `json:"invoice_id"`
	CustomerID     string          `json:"customer_id"`
	SubscriptionID *string         `json:"subscription_id,omitempty"`
	PriceID        string          `json:"price_id"`
	MeterID        *string         `json:"meter_id,omitempty"`
	Amount         decimal.Decimal `json:"amount"`
	Quantity       decimal.Decimal `json:"quantity"`
	Currency       string          `json:"currency"`
	PeriodStart    *time.Time      `json:"period_start,omitempty"`
	PeriodEnd      *time.Time      `json:"period_end,omitempty"`
	Metadata       types.Metadata  `json:"metadata,omitempty"`
	types.BaseModel
}

// FromEnt converts an ent.InvoiceLineItem to domain InvoiceLineItem
func (i *InvoiceLineItem) FromEnt(e *ent.InvoiceLineItem) *InvoiceLineItem {
	if e == nil {
		return nil
	}

	return &InvoiceLineItem{
		ID:             e.ID,
		InvoiceID:      e.InvoiceID,
		CustomerID:     e.CustomerID,
		SubscriptionID: e.SubscriptionID,
		PriceID:        e.PriceID,
		MeterID:        e.MeterID,
		Amount:         e.Amount,
		Quantity:       e.Quantity,
		Currency:       e.Currency,
		PeriodStart:    e.PeriodStart,
		PeriodEnd:      e.PeriodEnd,
		Metadata:       e.Metadata,
		BaseModel: types.BaseModel{
			TenantID:  e.TenantID,
			Status:    types.Status(e.Status),
			CreatedBy: e.CreatedBy,
			UpdatedBy: e.UpdatedBy,
			CreatedAt: e.CreatedAt,
			UpdatedAt: e.UpdatedAt,
		},
	}
}

// Validate validates the invoice line item
func (i *InvoiceLineItem) Validate() error {
	if i.Amount.IsNegative() {
		return NewValidationError("amount", "must be non negative")
	}

	if i.Quantity.IsNegative() {
		return NewValidationError("quantity", "must be non negative")
	}

	if i.PeriodStart != nil && i.PeriodEnd != nil {
		if i.PeriodEnd.Before(*i.PeriodStart) {
			return NewValidationError("period_end", "must be after period_start")
		}
	}

	return nil
}
