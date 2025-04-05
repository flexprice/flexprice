package invoice

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// InvoiceLineItem represents a single line item in an invoice
type InvoiceLineItem struct {
	ID               string          `json:"id"`
	InvoiceID        string          `json:"invoice_id"`
	CustomerID       string          `json:"customer_id"`
	SubscriptionID   *string         `json:"subscription_id,omitempty"`
	PlanID           *string         `json:"plan_id,omitempty"`
	PlanDisplayName  *string         `json:"plan_display_name,omitempty"`
	PriceID          *string         `json:"price_id,omitempty"`
	PriceType        *string         `json:"price_type,omitempty"`
	MeterID          *string         `json:"meter_id,omitempty"`
	MeterDisplayName *string         `json:"meter_display_name,omitempty"`
	DisplayName      *string         `json:"display_name,omitempty"`
	Amount           decimal.Decimal `json:"amount"`
	Quantity         decimal.Decimal `json:"quantity"`
	Currency         string          `json:"currency"`
	PeriodStart      *time.Time      `json:"period_start,omitempty"`
	PeriodEnd        *time.Time      `json:"period_end,omitempty"`
	Metadata         types.Metadata  `json:"metadata,omitempty"`
	EnvironmentID    string          `json:"environment_id"`
	types.BaseModel
}

// FromEnt converts an ent.InvoiceLineItem to domain InvoiceLineItem
func (i *InvoiceLineItem) FromEnt(e *ent.InvoiceLineItem) *InvoiceLineItem {
	if e == nil {
		return nil
	}

	return &InvoiceLineItem{
		ID:               e.ID,
		InvoiceID:        e.InvoiceID,
		CustomerID:       e.CustomerID,
		SubscriptionID:   e.SubscriptionID,
		PlanID:           e.PlanID,
		PlanDisplayName:  e.PlanDisplayName,
		PriceID:          e.PriceID,
		PriceType:        e.PriceType,
		MeterID:          e.MeterID,
		MeterDisplayName: e.MeterDisplayName,
		DisplayName:      e.DisplayName,
		Amount:           e.Amount,
		Quantity:         e.Quantity,
		Currency:         e.Currency,
		PeriodStart:      e.PeriodStart,
		PeriodEnd:        e.PeriodEnd,
		Metadata:         e.Metadata,
		EnvironmentID:    e.EnvironmentID,
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
		return ierr.NewError("invoice line item validation failed").WithHint("amount must be non negative").Mark(ierr.ErrValidation)
	}

	if i.Quantity.IsNegative() {
		return ierr.NewError("invoice line item validation failed").WithHint("quantity must be non negative").Mark(ierr.ErrValidation)
	}

	if i.PeriodStart != nil && i.PeriodEnd != nil {
		if i.PeriodEnd.Before(*i.PeriodStart) {
			return ierr.NewError("invoice line item validation failed").WithHint("period_end must be after period_start").Mark(ierr.ErrValidation)
		}
	}

	return nil
}
