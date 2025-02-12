package subscription

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/types"
)

type Subscription struct {
	// ID is the unique identifier for the subscription
	ID string `db:"id" json:"id"`

	// LookupKey is the key used to lookup the subscription in our system
	LookupKey string `db:"lookup_key" json:"lookup_key,omitempty"`

	// CustomerID is the identifier for the customer in our system
	CustomerID string `db:"customer_id" json:"customer_id"`

	// PlanID is the identifier for the plan in our system
	PlanID string `db:"plan_id" json:"plan_id"`

	// Status is the status of the subscription
	SubscriptionStatus types.SubscriptionStatus `db:"subscription_status" json:"subscription_status"`

	// Currency is the currency of the subscription in lowercase 3 digit ISO codes
	Currency string `db:"currency" json:"currency"`

	// BillingAnchor is the reference point that aligns future billing cycle dates.
	// It sets the day of week for week intervals, the day of month for month and year intervals,
	// and the month of year for year intervals. The timestamp is in UTC format.
	BillingAnchor time.Time `db:"billing_anchor" json:"billing_anchor"`

	// StartDate is the start date of the subscription
	StartDate time.Time `db:"start_date" json:"start_date"`

	// EndDate is the end date of the subscription
	EndDate *time.Time `db:"end_date" json:"end_date,omitempty"`

	// CurrentPeriodStart is the end of the current period that the subscription has been invoiced for.
	// At the end of this period, a new invoice will be created.
	CurrentPeriodStart time.Time `db:"current_period_start" json:"current_period_start"`

	// CurrentPeriodEnd is the end of the current period that the subscription has been invoiced for.
	// At the end of this period, a new invoice will be created.
	CurrentPeriodEnd time.Time `db:"current_period_end" json:"current_period_end"`

	// CanceledAt is the date the subscription was canceled
	CancelledAt *time.Time `db:"cancelled_at" json:"cancelled_at,omitempty"`

	// CancelAt is the date the subscription will be canceled
	CancelAt *time.Time `db:"cancel_at" json:"cancel_at"`

	// CancelAtPeriodEnd is whether the subscription was canceled at the end of the current period
	CancelAtPeriodEnd bool `db:"cancel_at_period_end" json:"cancel_at_period_end"`

	// TrialStart is the start date of the trial period
	TrialStart *time.Time `db:"trial_start" json:"trial_start"`

	// TrialEnd is the end date of the trial period
	TrialEnd *time.Time `db:"trial_end" json:"trial_end"`

	// BillingCadence is the cadence of the billing cycle.
	BillingCadence types.BillingCadence `db:"billing_cadence" json:"billing_cadence"`

	// BillingPeriod is the period of the billing cycle.
	BillingPeriod types.BillingPeriod `db:"billing_period" json:"billing_period"`

	// BillingPeriodCount is the total number units of the billing period.
	BillingPeriodCount int `db:"billing_period_count" json:"billing_period_count"`

	// InvoiceCadence is the cadence of the invoice. This overrides the plan's invoice cadence.
	InvoiceCadence types.InvoiceCadence `db:"invoice_cadence" json:"invoice_cadence"`

	// Version is used for optimistic locking
	Version int `db:"version" json:"version"`

	// Metadata is a map of key-value pairs that can be attached to the subscription
	Metadata types.Metadata `db:"metadata" json:"metadata,omitempty"`

	LineItems []*SubscriptionLineItem `json:"line_items,omitempty"`

	types.BaseModel
}

func GetSubscriptionFromEnt(sub *ent.Subscription) *Subscription {
	var lineItems []*SubscriptionLineItem
	if sub.Edges.LineItems != nil {
		lineItems = make([]*SubscriptionLineItem, len(sub.Edges.LineItems))
		for i, item := range sub.Edges.LineItems {
			lineItems[i] = SubscriptionLineItemFromEnt(item)
		}
	}

	return &Subscription{
		ID:                 sub.ID,
		LookupKey:          sub.LookupKey,
		CustomerID:         sub.CustomerID,
		PlanID:             sub.PlanID,
		SubscriptionStatus: types.SubscriptionStatus(sub.SubscriptionStatus),
		Currency:           sub.Currency,
		BillingAnchor:      sub.BillingAnchor,
		StartDate:          sub.StartDate,
		EndDate:            sub.EndDate,
		CurrentPeriodStart: sub.CurrentPeriodStart,
		CurrentPeriodEnd:   sub.CurrentPeriodEnd,
		CancelledAt:        sub.CancelledAt,
		CancelAt:           sub.CancelAt,
		CancelAtPeriodEnd:  sub.CancelAtPeriodEnd,
		TrialStart:         sub.TrialStart,
		TrialEnd:           sub.TrialEnd,
		InvoiceCadence:     types.InvoiceCadence(sub.InvoiceCadence),
		BillingCadence:     types.BillingCadence(sub.BillingCadence),
		BillingPeriod:      types.BillingPeriod(sub.BillingPeriod),
		BillingPeriodCount: sub.BillingPeriodCount,
		Version:            sub.Version,
		Metadata:           sub.Metadata,
		LineItems:          lineItems,
		BaseModel: types.BaseModel{
			TenantID:  sub.TenantID,
			Status:    types.Status(sub.Status),
			CreatedAt: sub.CreatedAt,
			CreatedBy: sub.CreatedBy,
			UpdatedAt: sub.UpdatedAt,
			UpdatedBy: sub.UpdatedBy,
		},
	}
}
