package subscription

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/shopspring/decimal"
)

// SubscriptionLineItem represents a line item in a subscription
type SubscriptionLineItem struct {
	ID               string               `db:"id" json:"id"`
	SubscriptionID   string               `db:"subscription_id" json:"subscription_id"`
	CustomerID       string               `db:"customer_id" json:"customer_id"`
	PlanID           string               `db:"plan_id" json:"plan_id,omitempty"`
	PlanDisplayName  string               `db:"plan_display_name" json:"plan_display_name,omitempty"`
	PriceID          string               `db:"price_id" json:"price_id"`
	PriceType        types.PriceType      `db:"price_type" json:"price_type,omitempty"`
	MeterID          string               `db:"meter_id" json:"meter_id,omitempty"`
	MeterDisplayName string               `db:"meter_display_name" json:"meter_display_name,omitempty"`
	DisplayName      string               `db:"display_name" json:"display_name,omitempty"`
	Quantity         decimal.Decimal      `db:"quantity" json:"quantity"`
	Currency         string               `db:"currency" json:"currency"`
	BillingPeriod    types.BillingPeriod  `db:"billing_period" json:"billing_period"`
	InvoiceCadence   types.InvoiceCadence `db:"invoice_cadence" json:"invoice_cadence"`
	TrialPeriod      int                  `db:"trial_period" json:"trial_period"`
	StartDate        time.Time            `db:"start_date" json:"start_date,omitempty"`
	EndDate          time.Time            `db:"end_date" json:"end_date,omitempty"`
	Metadata         map[string]string    `db:"metadata" json:"metadata,omitempty"`
	EnvironmentID    string               `db:"environment_id" json:"environment_id"`
	types.BaseModel
}

// IsActive returns true if the line item is active
func (li *SubscriptionLineItem) IsActive() bool {
	if li.Status != types.StatusPublished {
		return false
	}
	now := time.Now()
	if li.StartDate.IsZero() {
		return false
	}

	if li.StartDate.After(now) {
		return false
	}

	if !li.EndDate.IsZero() && li.EndDate.Before(now) {
		return false
	}
	return true
}

func (li *SubscriptionLineItem) IsUsage() bool {
	return li.PriceType == types.PRICE_TYPE_USAGE && li.MeterID != ""
}

// FromEntList converts a list of Ent SubscriptionLineItems to domain SubscriptionLineItems
func GetLineItemFromEntList(list []*ent.SubscriptionLineItem) []*SubscriptionLineItem {
	if list == nil {
		return nil
	}
	items := make([]*SubscriptionLineItem, len(list))
	for i, item := range list {
		items[i] = SubscriptionLineItemFromEnt(item)
	}
	return items
}

// SubscriptionLineItemFromEnt converts an ent.SubscriptionLineItem to domain SubscriptionLineItem
func SubscriptionLineItemFromEnt(e *ent.SubscriptionLineItem) *SubscriptionLineItem {
	if e == nil {
		return nil
	}

	var planID, planDisplayName, priceType, meterID, meterDisplayName, displayName string
	var startDate, endDate time.Time

	if e.PlanID != nil {
		planID = *e.PlanID
	}
	if e.PlanDisplayName != nil {
		planDisplayName = *e.PlanDisplayName
	}
	if e.PriceType != nil {
		priceType = *e.PriceType
	}
	if e.MeterID != nil {
		meterID = *e.MeterID
	}
	if e.MeterDisplayName != nil {
		meterDisplayName = *e.MeterDisplayName
	}
	if e.DisplayName != nil {
		displayName = *e.DisplayName
	}
	if e.StartDate != nil {
		startDate = *e.StartDate
	}
	if e.EndDate != nil {
		endDate = *e.EndDate
	}

	return &SubscriptionLineItem{
		ID:               e.ID,
		SubscriptionID:   e.SubscriptionID,
		CustomerID:       e.CustomerID,
		PlanID:           planID,
		PlanDisplayName:  planDisplayName,
		PriceID:          e.PriceID,
		PriceType:        types.PriceType(priceType),
		MeterID:          meterID,
		MeterDisplayName: meterDisplayName,
		DisplayName:      displayName,
		Quantity:         e.Quantity,
		Currency:         e.Currency,
		BillingPeriod:    types.BillingPeriod(e.BillingPeriod),
		InvoiceCadence:   types.InvoiceCadence(e.InvoiceCadence),
		TrialPeriod:      e.TrialPeriod,
		StartDate:        startDate,
		EndDate:          endDate,
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
