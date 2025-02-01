package dto

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/go-playground/validator/v10"
)

type CreateSubscriptionRequest struct {
	CustomerID         string               `json:"customer_id" validate:"required"`
	PlanID             string               `json:"plan_id" validate:"required"`
	Currency           string               `json:"currency" validate:"required,len=3"`
	LookupKey          string               `json:"lookup_key"`
	StartDate          time.Time            `json:"start_date" validate:"required"`
	EndDate            *time.Time           `json:"end_date,omitempty"`
	TrialStart         *time.Time           `json:"trial_start,omitempty"`
	TrialEnd           *time.Time           `json:"trial_end,omitempty"`
	InvoiceCadence     types.InvoiceCadence `json:"invoice_cadence" validate:"required"`
	BillingCadence     types.BillingCadence `json:"billing_cadence" validate:"required"`
	BillingPeriod      types.BillingPeriod  `json:"billing_period" validate:"required"`
	BillingPeriodCount int                  `json:"billing_period_count" validate:"required,min=1"`
}

type UpdateSubscriptionRequest struct {
	Status            types.SubscriptionStatus `json:"status"`
	CancelAt          *time.Time               `json:"cancel_at,omitempty"`
	CancelAtPeriodEnd bool                     `json:"cancel_at_period_end,omitempty"`
}

type SubscriptionResponse struct {
	*subscription.Subscription
	Plan     *PlanResponse     `json:"plan"`
	Customer *CustomerResponse `json:"customer"`
}

// ListSubscriptionsResponse represents the response for listing subscriptions
type ListSubscriptionsResponse = types.ListResponse[*SubscriptionResponse]

func (r *CreateSubscriptionRequest) Validate() error {
	err := validator.New().Struct(r)
	if err != nil {
		return err
	}

	if r.StartDate.After(time.Now().UTC()) {
		return fmt.Errorf("start_date: can not be in the future - %s", r.StartDate)
	}

	if r.EndDate != nil && r.EndDate.Before(r.StartDate) {
		return fmt.Errorf("end_date: can not be before start_date - %s", r.EndDate)
	}

	if r.TrialStart != nil && r.TrialStart.After(r.StartDate) {
		return fmt.Errorf("trial_start: can not be after start_date - %s", r.TrialStart)
	}

	if r.TrialEnd != nil && r.TrialEnd.Before(r.StartDate) {
		return fmt.Errorf("trial_end: can not be before start_date - %s", r.TrialEnd)
	}

	return nil
}

func (r *CreateSubscriptionRequest) ToSubscription(ctx context.Context) *subscription.Subscription {
	now := time.Now().UTC()
	if r.StartDate.IsZero() {
		r.StartDate = now
	}

	return &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
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

type SubscriptionUpdatePeriodResponse struct {
	TotalSuccess int                                     `json:"total_success"`
	TotalFailed  int                                     `json:"total_failed"`
	Items        []*SubscriptionUpdatePeriodResponseItem `json:"items"`
	StartAt      time.Time                               `json:"start_at"`
}

type SubscriptionUpdatePeriodResponseItem struct {
	SubscriptionID string    `json:"subscription_id"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	Success        bool      `json:"success"`
	Error          string    `json:"error"`
}
