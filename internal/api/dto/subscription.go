package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
)

type CreateSubscriptionRequest struct {
	CustomerID         string               `json:"customer_id" validate:"required"`
	PlanID             string               `json:"plan_id" validate:"required"`
	Currency           string               `json:"currency"`
	LookupKey          string               `json:"lookup_key"`
	StartDate          time.Time            `json:"start_date,omitempty"`
	EndDate            *time.Time           `json:"end_date,omitempty"`
	TrialStart         *time.Time           `json:"trial_start,omitempty"`
	TrialEnd           *time.Time           `json:"trial_end,omitempty"`
	InvoiceCadence     types.InvoiceCadence `json:"invoice_cadence,omitempty"`
	BillingCadence     types.BillingCadence `json:"billing_cadence,omitempty"`
	BillingPeriod      types.BillingPeriod  `json:"billing_period,omitempty"`
	BillingPeriodCount int                  `json:"billing_period_count,omitempty"`
}

type UpdateSubscriptionRequest struct {
	Status            types.SubscriptionStatus `json:"status"`
	CancelAt          *time.Time               `json:"cancel_at,omitempty"`
	CancelAtPeriodEnd bool                     `json:"cancel_at_period_end,omitempty"`
}

type SubscriptionResponse struct {
	*subscription.Subscription
	Plan *PlanResponse `json:"plan"`
}

type ListSubscriptionsResponse struct {
	Subscriptions []*SubscriptionResponse `json:"subscriptions"`
	Total         int                     `json:"total"`
	Offset        int                     `json:"offset"`
	Limit         int                     `json:"limit"`
}

func (r *CreateSubscriptionRequest) Validate() error {
	return validator.New().Struct(r)
}

func (r *CreateSubscriptionRequest) ToSubscription(ctx context.Context) *subscription.Subscription {
	now := time.Now().UTC()
	if r.StartDate.IsZero() {
		r.StartDate = now
	}

	return &subscription.Subscription{
		ID:                 uuid.New().String(),
		CustomerID:         r.CustomerID,
		PlanID:             r.PlanID,
		Currency:           strings.ToLower(r.Currency),
		LookupKey:          r.LookupKey,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          r.StartDate,
		EndDate:            r.EndDate,
		TrialStart:         r.TrialStart,
		TrialEnd:           r.TrialEnd,
		InvoiceCadence:     r.InvoiceCadence,
		BillingCadence:     r.BillingCadence,
		BillingPeriod:      r.BillingPeriod,
		BillingPeriodCount: r.BillingPeriodCount,
		BillingAnchor:      r.StartDate,
		BaseModel:          types.GetDefaultBaseModel(ctx),
	}
}

type GetUsageBySubscriptionRequest struct {
	SubscriptionID string    `json:"subscription_id" binding:"required" example:"123"`
	StartTime      time.Time `json:"start_time" example:"2024-03-13T00:00:00Z"`
	EndTime        time.Time `json:"end_time" example:"2024-03-20T00:00:00Z"`
	LifetimeUsage  bool      `json:"lifetime_usage" example:"false"`
}

type GetUsageBySubscriptionResponse struct {
	Amount        float64                              `json:"amount"`
	Currency      string                               `json:"currency"`
	DisplayAmount string                               `json:"display_amount"`
	StartTime     time.Time                            `json:"start_time"`
	EndTime       time.Time                            `json:"end_time"`
	Charges       []*SubscriptionUsageByMetersResponse `json:"charges"`
}

type SubscriptionUsageByMetersResponse struct {
	Amount           float64            `json:"amount"`
	Currency         string             `json:"currency"`
	DisplayAmount    string             `json:"display_amount"`
	Quantity         float64            `json:"quantity"`
	FilterValues     price.JSONBFilters `json:"filter_values"`
	MeterDisplayName string             `json:"meter_display_name"`
	Price            *price.Price       `json:"price"`
}
