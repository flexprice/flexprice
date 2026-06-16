package dto

import "github.com/flexprice/flexprice/internal/types"

// CreateCheckoutRequest opens a checkout for a NEW subscription (payment objective in v1).
type CreateCheckoutRequest struct {
	CustomerID         string                  `json:"customer_id" binding:"required"`
	PlanID             string                  `json:"plan_id" binding:"required"`
	Currency           string                  `json:"currency" binding:"required"`
	Objective          types.CheckoutObjective `json:"objective" binding:"required"`
	BillingPeriod      types.BillingPeriod     `json:"billing_period" binding:"required"`
	BillingPeriodCount int                     `json:"billing_period_count,omitempty"`
	SuccessURL         string                  `json:"success_url,omitempty"`
	CancelURL          string                  `json:"cancel_url,omitempty"`
	SaveCard           bool                    `json:"save_card,omitempty"`
	Metadata           map[string]string       `json:"metadata,omitempty"`
}

// CreateSubscriptionChangeCheckoutRequest opens a payment-gated checkout for an
// in-place plan UPGRADE of an existing subscription.
type CreateSubscriptionChangeCheckoutRequest struct {
	TargetPlanID      string                  `json:"target_plan_id" binding:"required"`
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`
	SuccessURL        string                  `json:"success_url,omitempty"`
	CancelURL         string                  `json:"cancel_url,omitempty"`
	Metadata          map[string]string       `json:"metadata,omitempty"`
}

type CheckoutResponse struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	CheckoutURL string `json:"checkout_url"`
}
