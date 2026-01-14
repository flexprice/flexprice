package invoice

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// InvoiceLineItem represents a single line item in an invoice
type InvoiceLineItem struct {
	ID               string                `json:"id"`
	InvoiceID        string                `json:"invoice_id"`
	CustomerID       string                `json:"customer_id"`
	SubscriptionID   *string               `json:"subscription_id,omitempty"`
	EntityID         *string               `json:"entity_id,omitempty"`
	EntityType       *string               `json:"entity_type,omitempty"`
	PlanDisplayName  *string               `json:"plan_display_name,omitempty"`
	PriceID          *string               `json:"price_id,omitempty"`
	PriceType        *string               `json:"price_type,omitempty"`
	MeterID          *string               `json:"meter_id,omitempty"`
	MeterDisplayName *string               `json:"meter_display_name,omitempty"`
	PriceUnitID      *string               `json:"price_unit_id,omitempty"`
	PriceUnit        *string               `json:"price_unit,omitempty"`
	PriceUnitAmount  *decimal.Decimal      `json:"price_unit_amount,omitempty"`
	DisplayName      *string               `json:"display_name,omitempty"`
	Amount           decimal.Decimal       `json:"amount"`
	Quantity         decimal.Decimal       `json:"quantity"`
	Currency         string                `json:"currency"`
	PeriodStart      *time.Time            `json:"period_start,omitempty"`
	PeriodEnd        *time.Time            `json:"period_end,omitempty"`
	Metadata         types.Metadata        `json:"metadata,omitempty"`
	EnvironmentID    string                `json:"environment_id"`
	CommitmentInfo   *types.CommitmentInfo `json:"commitment_info,omitempty"`

	// prepaid_credits_applied is the amount in invoice currency reduced from this line item due to prepaid credits application.
	PrepaidCreditsApplied decimal.Decimal `json:"prepaid_credits_applied"`

	// line_item_discount is the discount amount in invoice currency applied directly to this line item.
	LineItemDiscount decimal.Decimal `json:"line_item_discount"`

	// invoice_level_discount is the discount amount in invoice currency applied to all line items on the invoice.
	InvoiceLevelDiscount decimal.Decimal `json:"invoice_level_discount"`

	types.BaseModel
}

// FromEnt converts an ent.InvoiceLineItem to domain InvoiceLineItem
func (i *InvoiceLineItem) FromEnt(e *ent.InvoiceLineItem) *InvoiceLineItem {
	if e == nil {
		return nil
	}

	return &InvoiceLineItem{
		ID:                    e.ID,
		InvoiceID:             e.InvoiceID,
		CustomerID:            e.CustomerID,
		SubscriptionID:        e.SubscriptionID,
		EntityID:              e.EntityID,
		EntityType:            lo.ToPtr(string(lo.FromPtr(e.EntityType))),
		PlanDisplayName:       e.PlanDisplayName,
		PriceID:               e.PriceID,
		PriceType:             lo.ToPtr(string(lo.FromPtr(e.PriceType))),
		MeterID:               e.MeterID,
		MeterDisplayName:      e.MeterDisplayName,
		PriceUnitID:           e.PriceUnitID,
		PriceUnit:             e.PriceUnit,
		PriceUnitAmount:       e.PriceUnitAmount,
		DisplayName:           e.DisplayName,
		Amount:                e.Amount,
		Quantity:              e.Quantity,
		Currency:              e.Currency,
		PeriodStart:           e.PeriodStart,
		PeriodEnd:             e.PeriodEnd,
		Metadata:              e.Metadata,
		CommitmentInfo:        e.CommitmentInfo,
		EnvironmentID:         e.EnvironmentID,
		PrepaidCreditsApplied: lo.FromPtrOr(e.PrepaidCreditsApplied, decimal.Zero),
		LineItemDiscount:      lo.FromPtrOr(e.LineItemDiscount, decimal.Zero),
		InvoiceLevelDiscount:  lo.FromPtrOr(e.InvoiceLevelDiscount, decimal.Zero),
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

	// Validate invoice_level_discount: must be non-negative (zero is allowed, meaning no discount)
	if i.InvoiceLevelDiscount.IsNegative() {
		return ierr.NewError("invoice line item validation failed").
			WithHint("invoice_level_discount must be non-negative").
			WithReportableDetails(map[string]any{
				"invoice_level_discount": i.InvoiceLevelDiscount.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}
