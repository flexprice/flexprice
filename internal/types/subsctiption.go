package types

import "time"

// SubscriptionStatus is the status of a subscription
// For now taking inspiration from Stripe's subscription statuses
// https://stripe.com/docs/api/subscriptions/object#subscription_object-status
type SubscriptionStatus string

const (
	SubscriptionStatusActive            SubscriptionStatus = "active"
	SubscriptionStatusPaused            SubscriptionStatus = "paused"
	SubscriptionStatusCancelled         SubscriptionStatus = "cancelled"
	SubscriptionStatusIncomplete        SubscriptionStatus = "incomplete"
	SubscriptionStatusIncompleteExpired SubscriptionStatus = "incomplete_expired"
	SubscriptionStatusPastDue           SubscriptionStatus = "past_due"
	SubscriptionStatusTrialing          SubscriptionStatus = "trialing"
	SubscriptionStatusUnpaid            SubscriptionStatus = "unpaid"
)

type SubscriptionFilter struct {
	Filter
	CustomerID             string             `form:"customer_id"`
	SubscriptionStatus     SubscriptionStatus `form:"subscription_status"`
	Status                 Status             `form:"status"`
	PlanID                 string             `form:"plan_id"`
	CurrentPeriodEndBefore *time.Time         `form:"current_period_end_before"`
}

// ToMap only parsing selected fields which can be supported by ClickHouse
// as built in our query_builder.go
func (f *SubscriptionFilter) ToMap() map[string]interface{} {
	params := map[string]interface{}{
		"offset": f.Offset,
		"limit":  f.Limit,
	}

	if f.CustomerID != "" {
		params["customer_id"] = f.CustomerID
	}

	if f.SubscriptionStatus != "" {
		params["subscription_status"] = f.SubscriptionStatus
	}

	if f.Status != "" {
		params["status"] = f.Status
	}

	if f.PlanID != "" {
		params["plan_id"] = f.PlanID
	}

	return params
}
