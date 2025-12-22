package subscription

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

// SubscriptionLineItem represents a line item in a subscription
type SubscriptionLineItem struct {
	ID                  string                               `db:"id" json:"id"`
	SubscriptionID      string                               `db:"subscription_id" json:"subscription_id"`
	CustomerID          string                               `db:"customer_id" json:"customer_id"`
	EntityID            string                               `db:"entity_id" json:"entity_id,omitempty"`
	EntityType          types.SubscriptionLineItemEntityType `db:"entity_type" json:"entity_type,omitempty"`
	PlanDisplayName     string                               `db:"plan_display_name" json:"plan_display_name,omitempty"`
	PriceID             string                               `db:"price_id" json:"price_id"`
	PriceType           types.PriceType                      `db:"price_type" json:"price_type,omitempty"`
	MeterID             string                               `db:"meter_id" json:"meter_id,omitempty"`
	MeterDisplayName    string                               `db:"meter_display_name" json:"meter_display_name,omitempty"`
	PriceUnitID         *string                              `db:"price_unit_id" json:"price_unit_id"`
	PriceUnit           *string                              `db:"price_unit" json:"price_unit"`
	DisplayName         string                               `db:"display_name" json:"display_name,omitempty"`
	Quantity            decimal.Decimal                      `db:"quantity" json:"quantity" swaggertype:"string"`
	Currency            string                               `db:"currency" json:"currency"`
	BillingPeriod       types.BillingPeriod                  `db:"billing_period" json:"billing_period"`
	InvoiceCadence      types.InvoiceCadence                 `db:"invoice_cadence" json:"invoice_cadence"`
	TrialPeriod         int                                  `db:"trial_period" json:"trial_period"`
	StartDate           time.Time                            `db:"start_date" json:"start_date,omitempty"`
	EndDate             time.Time                            `db:"end_date" json:"end_date,omitempty"`
	SubscriptionPhaseID *string                              `db:"subscription_phase_id" json:"subscription_phase_id,omitempty"`
	Metadata            map[string]string                    `db:"metadata" json:"metadata,omitempty"`
	EnvironmentID       string                               `db:"environment_id" json:"environment_id"`

	// Commitment fields
	CommitmentAmount        *decimal.Decimal     `db:"commitment_amount" json:"commitment_amount,omitempty"`
	CommitmentQuantity      *decimal.Decimal     `db:"commitment_quantity" json:"commitment_quantity,omitempty"`
	CommitmentType          types.CommitmentType `db:"commitment_type" json:"commitment_type,omitempty"`
	CommitmentOverageFactor *decimal.Decimal     `db:"commitment_overage_factor" json:"commitment_overage_factor,omitempty"`
	CommitmentTrueUpEnabled bool                 `db:"commitment_true_up_enabled" json:"commitment_true_up_enabled"`
	CommitmentWindowed      bool                 `db:"commitment_windowed" json:"commitment_windowed"`

	Price *price.Price `json:"price,omitempty"`

	types.BaseModel
}

// IsActive returns true if the line item is active
// to check if the line item is active and is mostly used with time.Now()
// and in case of event post processing, we pass the event timestamp
func (li *SubscriptionLineItem) IsActive(t time.Time) bool {
	if li.Status != types.StatusPublished {
		return false
	}
	if li.StartDate.IsZero() {
		return false
	}

	if li.StartDate.After(t) {
		return false
	}

	if !li.EndDate.IsZero() && li.EndDate.Before(t) {
		return false
	}
	return true
}

func (li *SubscriptionLineItem) IsUsage() bool {
	return li.PriceType == types.PRICE_TYPE_USAGE && li.MeterID != ""
}

// HasCommitment returns true if the line item has commitment configured
func (li *SubscriptionLineItem) HasCommitment() bool {
	hasAmountCommitment := li.CommitmentAmount != nil && li.CommitmentAmount.GreaterThan(decimal.Zero)
	hasQuantityCommitment := li.CommitmentQuantity != nil && li.CommitmentQuantity.GreaterThan(decimal.Zero)
	return hasAmountCommitment || hasQuantityCommitment
}

// GetCommitmentType returns the commitment type for the line item
func (li *SubscriptionLineItem) GetCommitmentType() types.CommitmentType {
	return li.CommitmentType
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

	var meterID, meterDisplayName, displayName string
	var startDate, endDate time.Time
	var subscriptionPhaseID *string

	priceType := lo.FromPtr(e.PriceType)
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
	if e.SubscriptionPhaseID != nil {
		subscriptionPhaseID = e.SubscriptionPhaseID
	}

	// Handle commitment fields
	var commitmentType types.CommitmentType
	if e.CommitmentType != nil {
		commitmentType = types.CommitmentType(*e.CommitmentType)
	}

	return &SubscriptionLineItem{
		ID:                      e.ID,
		SubscriptionID:          e.SubscriptionID,
		CustomerID:              e.CustomerID,
		EntityID:                lo.FromPtr(e.EntityID),
		EntityType:              types.SubscriptionLineItemEntityType(e.EntityType),
		PlanDisplayName:         lo.FromPtr(e.PlanDisplayName),
		PriceID:                 e.PriceID,
		PriceType:               priceType,
		MeterID:                 meterID,
		MeterDisplayName:        meterDisplayName,
		PriceUnitID:             e.PriceUnitID,
		PriceUnit:               e.PriceUnit,
		DisplayName:             displayName,
		Quantity:                e.Quantity,
		Currency:                e.Currency,
		BillingPeriod:           e.BillingPeriod,
		InvoiceCadence:          e.InvoiceCadence,
		TrialPeriod:             e.TrialPeriod,
		StartDate:               startDate,
		EndDate:                 endDate,
		SubscriptionPhaseID:     subscriptionPhaseID,
		Metadata:                e.Metadata,
		EnvironmentID:           e.EnvironmentID,
		CommitmentAmount:        e.CommitmentAmount,
		CommitmentQuantity:      e.CommitmentQuantity,
		CommitmentType:          commitmentType,
		CommitmentOverageFactor: e.CommitmentOverageFactor,
		CommitmentTrueUpEnabled: e.CommitmentTrueUpEnabled,
		CommitmentWindowed:      e.CommitmentWindowed,
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

// GetPeriod returns period start and end dates based on line item dates
func (li *SubscriptionLineItem) GetPeriod(defaultPeriodStart, defaultPeriodEnd time.Time) (time.Time, time.Time) {
	return li.GetPeriodStart(defaultPeriodStart), li.GetPeriodEnd(defaultPeriodEnd)
}

// GetPeriodStart returns the period start date based on line item dates
func (li *SubscriptionLineItem) GetPeriodStart(defaultPeriodStart time.Time) time.Time {
	// If line item has a start date after default period start, use line item start date
	if !li.StartDate.IsZero() && (li.StartDate.After(defaultPeriodStart) || li.StartDate.Equal(defaultPeriodStart)) {
		return li.StartDate
	}
	return defaultPeriodStart
}

// GetPeriodEnd returns the period end date based on line item dates
func (li *SubscriptionLineItem) GetPeriodEnd(defaultPeriodEnd time.Time) time.Time {
	// If line item has an end date before default period end, use line item end date
	if !li.EndDate.IsZero() && (li.EndDate.Before(defaultPeriodEnd) || li.EndDate.Equal(defaultPeriodEnd)) {
		return li.EndDate
	}
	return defaultPeriodEnd
}
