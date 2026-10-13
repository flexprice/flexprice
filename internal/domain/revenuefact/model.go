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
	ID string `db:"id" json:"id"`

	TenantID      string `db:"tenant_id" json:"tenant_id"`
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	CustomerID     string  `db:"customer_id" json:"customer_id"`
	SubscriptionID string  `db:"subscription_id" json:"subscription_id"`
	SubLineItemID  *string `db:"sub_line_item_id" json:"sub_line_item_id,omitempty"`
	PriceID        *string `db:"price_id" json:"price_id,omitempty"`
	MeterID        *string `db:"meter_id" json:"meter_id,omitempty"`

	AggregationType *types.AggregationType `db:"aggregation_type" json:"aggregation_type,omitempty"`
	RevenueSource   types.RevenueSource    `db:"revenue_source" json:"revenue_source"`

	PeriodStart time.Time `db:"period_start" json:"period_start"`
	PeriodEnd   time.Time `db:"period_end" json:"period_end"`
	Day         time.Time `db:"day" json:"day"`

	ServiceStart *time.Time `db:"service_start" json:"service_start,omitempty"`
	ServiceEnd   *time.Time `db:"service_end" json:"service_end,omitempty"`

	RecognitionMethod *types.RecognitionMethod `db:"recognition_method" json:"recognition_method,omitempty"`

	UsageAtListRate   decimal.Decimal `db:"usage_at_list_rate" json:"usage_at_list_rate"`
	TierDelta         decimal.Decimal `db:"tier_delta" json:"tier_delta"`
	EntitlementAmount decimal.Decimal `db:"entitlement_amount" json:"entitlement_amount"`
	LineDiscount      decimal.Decimal `db:"line_discount" json:"line_discount"`
	InvoiceDiscount   decimal.Decimal `db:"invoice_discount" json:"invoice_discount"`
	NetAmount         decimal.Decimal `db:"net_amount" json:"net_amount"`
	BillableQty       decimal.Decimal `db:"billable_qty" json:"billable_qty"`
	EntitlementQty    decimal.Decimal `db:"entitlement_qty" json:"entitlement_qty"`

	DecompositionMode types.DecompositionMode `db:"decomposition_mode" json:"decomposition_mode"`
	Currency          string                  `db:"currency" json:"currency"`

	Status types.FactStatus `db:"status" json:"status"`

	IsRevert bool `db:"is_revert" json:"is_revert"`

	InvoiceID         *string `db:"invoice_id" json:"invoice_id,omitempty"`
	InvoiceLineItemID *string `db:"invoice_line_item_id" json:"invoice_line_item_id,omitempty"`

	// LockAdjustedDay is the Phase-4 recognition-posting day: equals day while
	// the period is open, and shifts to the next open period's first day once the
	// period is locked.
	LockAdjustedDay *time.Time `db:"lock_adjusted_day" json:"lock_adjusted_day,omitempty"`

	ComputedAt time.Time `db:"computed_at" json:"computed_at"`
	Version    int64     `db:"version" json:"version"`
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
