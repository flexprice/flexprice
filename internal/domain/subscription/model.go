package subscription

import (
	"time"

	"github.com/flexprice/flexprice/ent"
	"github.com/flexprice/flexprice/internal/domain/coupon_association"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
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

	SubscriptionStatus types.SubscriptionStatus `db:"subscription_status" json:"subscription_status"`

	// Currency is the currency of the subscription in lowercase 3 digit ISO codes
	Currency string `db:"currency" json:"currency"`

	// BillingAnchor is the reference point that aligns future billing cycle dates.
	// It sets the day of week for week intervals, the day of month for month and year intervals,
	// and the month of year for year intervals. The timestamp is in UTC format.
	BillingAnchor time.Time `db:"billing_anchor" json:"billing_anchor"`

	// BillingCycle is the cycle of the billing anchor.
	// This is used to determine the billing date for the subscription (i.e set the billing anchor)
	// If not set, the default value is anniversary. Possible values are anniversary and calendar.
	// Anniversary billing means the billing anchor will be the start date of the subscription.
	// Calendar billing means the billing anchor will be the appropriate date based on the billing period.
	// For example, if the billing period is month and the start date is 2025-04-15 then in case of
	// calendar billing the billing anchor will be 2025-05-01 vs 2025-04-15 for anniversary billing.
	BillingCycle types.BillingCycle `db:"billing_cycle" json:"billing_cycle"`

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

	BillingCadence types.BillingCadence `db:"billing_cadence" json:"billing_cadence"`

	BillingPeriod types.BillingPeriod `db:"billing_period" json:"billing_period"`

	// BillingPeriodCount is the total number units of the billing period.
	BillingPeriodCount int `db:"billing_period_count" json:"billing_period_count"`

	// Version is used for optimistic locking
	Version int `db:"version" json:"version"`

	Metadata types.Metadata `db:"metadata" json:"metadata,omitempty"`

	// EnvironmentID is the environment identifier for the subscription
	EnvironmentID string `db:"environment_id" json:"environment_id"`

	PauseStatus types.PauseStatus `db:"pause_status" json:"pause_status"`

	// ActivePauseID references the current active pause configuration
	// This will be null if no pause is active or scheduled
	ActivePauseID *string `db:"active_pause_id" json:"active_pause_id,omitempty"`

	// CommitmentAmount is the minimum amount a customer commits to paying for a billing period
	CommitmentAmount *decimal.Decimal `db:"commitment_amount" json:"commitment_amount,omitempty"`

	// OverageFactor is a multiplier applied to usage beyond the commitment amount
	OverageFactor *decimal.Decimal `db:"overage_factor" json:"overage_factor,omitempty"`

	LineItems []*SubscriptionLineItem `json:"line_items,omitempty"`

	Pauses []*SubscriptionPause `json:"pauses,omitempty"`

	CouponAssociations []*coupon_association.CouponAssociation `json:"coupon_associations,omitempty"`

	CustomerTimezone string `json:"customer_timezone"`

	ProrationMode types.ProrationMode `json:"proration_mode"`

	types.BaseModel
}

func FromEntList(subs []*ent.Subscription) []*Subscription {
	return lo.Map(subs, func(sub *ent.Subscription, _ int) *Subscription {
		return GetSubscriptionFromEnt(sub)
	})
}

func GetSubscriptionFromEnt(sub *ent.Subscription) *Subscription {
	var lineItems []*SubscriptionLineItem
	if sub.Edges.LineItems != nil {
		lineItems = make([]*SubscriptionLineItem, len(sub.Edges.LineItems))
		for i, item := range sub.Edges.LineItems {
			lineItems[i] = SubscriptionLineItemFromEnt(item)
		}
	}

	var pauses []*SubscriptionPause
	if sub.Edges.Pauses != nil {
		pauses = SubscriptionPauseListFromEnt(sub.Edges.Pauses)
	}

	var couponAssociations []*coupon_association.CouponAssociation
	if sub.Edges.CouponAssociations != nil {
		couponAssociations = coupon_association.FromEntList(sub.Edges.CouponAssociations)
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
		BillingCadence:     types.BillingCadence(sub.BillingCadence),
		BillingPeriod:      types.BillingPeriod(sub.BillingPeriod),
		BillingPeriodCount: sub.BillingPeriodCount,
		BillingCycle:       types.BillingCycle(sub.BillingCycle),
		Version:            sub.Version,
		Metadata:           sub.Metadata,
		EnvironmentID:      sub.EnvironmentID,
		PauseStatus:        types.PauseStatus(sub.PauseStatus),
		ActivePauseID:      sub.ActivePauseID,
		CommitmentAmount:   sub.CommitmentAmount,
		OverageFactor:      sub.OverageFactor,
		LineItems:          lineItems,
		CouponAssociations: couponAssociations,
		Pauses:             pauses,
		CustomerTimezone:   sub.CustomerTimezone,
		ProrationMode:      types.ProrationMode(sub.ProrationMode),
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
