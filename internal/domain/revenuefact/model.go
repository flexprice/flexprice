package revenuefact

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// RevenueFact is one row of the revenue_facts table: a day-grain slice of
// recognized/provisional revenue for a subscription line item.
type RevenueFact struct {
	ID string `json:"id"`

	TenantID      string `json:"tenant_id"`
	EnvironmentID string `json:"environment_id"`

	CustomerID     string  `json:"customer_id"`
	SubscriptionID string  `json:"subscription_id"`
	SubLineItemID  *string `json:"sub_line_item_id,omitempty"`
	PriceID        *string `json:"price_id,omitempty"`
	MeterID        *string `json:"meter_id,omitempty"`

	AggregationType *types.AggregationType `json:"aggregation_type,omitempty"`
	RevenueSource   types.RevenueSource    `json:"revenue_source"`

	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Day         time.Time `json:"day"`

	ServiceStart *time.Time `json:"service_start,omitempty"`
	ServiceEnd   *time.Time `json:"service_end,omitempty"`

	RecognitionMethod *types.RecognitionMethod `json:"recognition_method,omitempty"`

	UsageAtListRate   decimal.Decimal `json:"usage_at_list_rate"`
	TierDelta         decimal.Decimal `json:"tier_delta"`
	EntitlementAmount decimal.Decimal `json:"entitlement_amount"`
	LineDiscount      decimal.Decimal `json:"line_discount"`
	InvoiceDiscount   decimal.Decimal `json:"invoice_discount"`
	NetAmount         decimal.Decimal `json:"net_amount"`
	BillableQty       decimal.Decimal `json:"billable_qty"`
	EntitlementQty    decimal.Decimal `json:"entitlement_qty"`

	DecompositionMode types.DecompositionMode `json:"decomposition_mode"`
	Currency          string                  `json:"currency"`

	Status types.FactStatus `json:"status"`

	IsRevert bool `json:"is_revert"`

	InvoiceID         *string `json:"invoice_id,omitempty"`
	InvoiceLineItemID *string `json:"invoice_line_item_id,omitempty"`

	// LockAdjustedDay is the Phase-4 recognition-posting day: equals day while
	// the period is open, and shifts to the next open period's first day once the
	// period is locked.
	LockAdjustedDay *time.Time `json:"lock_adjusted_day,omitempty"`

	ComputedAt time.Time `json:"computed_at"`
	Version    int64     `json:"version"`
}

// FromEnt maps a generated ent.RevenueFact to the domain model.
func FromEnt(e *ent.RevenueFact) *RevenueFact {
	if e == nil {
		return nil
	}
	return &RevenueFact{
		ID:                e.ID,
		TenantID:          e.TenantID,
		EnvironmentID:     e.EnvironmentID,
		CustomerID:        e.CustomerID,
		SubscriptionID:    e.SubscriptionID,
		SubLineItemID:     e.SubLineItemID,
		PriceID:           e.PriceID,
		MeterID:           e.MeterID,
		AggregationType:   e.AggregationType,
		RevenueSource:     e.RevenueSource,
		PeriodStart:       e.PeriodStart,
		PeriodEnd:         e.PeriodEnd,
		Day:               e.Day,
		ServiceStart:      e.ServiceStart,
		ServiceEnd:        e.ServiceEnd,
		RecognitionMethod: e.RecognitionMethod,
		UsageAtListRate:   e.UsageAtListRate,
		TierDelta:         e.TierDelta,
		EntitlementAmount: e.EntitlementAmount,
		LineDiscount:      e.LineDiscount,
		InvoiceDiscount:   e.InvoiceDiscount,
		NetAmount:         e.NetAmount,
		BillableQty:       e.BillableQty,
		EntitlementQty:    e.EntitlementQty,
		DecompositionMode: e.DecompositionMode,
		Currency:          e.Currency,
		Status:            e.Status,
		IsRevert:          e.IsRevert,
		InvoiceID:         e.InvoiceID,
		InvoiceLineItemID: e.InvoiceLineItemID,
		LockAdjustedDay:   e.LockAdjustedDay,
		ComputedAt:        e.ComputedAt,
		Version:           e.Version,
	}
}

// FromEntList maps a list of generated ent.RevenueFact rows to domain models.
func FromEntList(rows []*ent.RevenueFact) []*RevenueFact {
	facts := make([]*RevenueFact, 0, len(rows))
	for _, row := range rows {
		facts = append(facts, FromEnt(row))
	}
	return facts
}
