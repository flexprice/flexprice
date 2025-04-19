package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/shopspring/decimal"
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
	BillingCadence     types.BillingCadence `json:"billing_cadence" validate:"required"`
	BillingPeriod      types.BillingPeriod  `json:"billing_period" validate:"required"`
	BillingPeriodCount int                  `json:"billing_period_count" validate:"required,min=1"`
	Metadata           map[string]string    `json:"metadata,omitempty"`
	// BillingCycle is the cycle of the billing anchor.
	// This is used to determine the billing date for the subscription (i.e set the billing anchor)
	// If not set, the default value is anniversary. Possible values are anniversary and calendar.
	// Anniversary billing means the billing anchor will be the start date of the subscription.
	// Calendar billing means the billing anchor will be the appropriate date based on the billing period.
	// For example, if the billing period is month and the start date is 2025-04-15 then in case of
	// calendar billing the billing anchor will be 2025-05-01 vs 2025-04-15 for anniversary billing.
	BillingCycle types.BillingCycle `json:"billing_cycle"`
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
	err := validator.ValidateRequest(r)
	if err != nil {
		return err
	}

	// Validate currency
	if err := types.ValidateCurrencyCode(r.Currency); err != nil {
		return err
	}

	if err := r.BillingCadence.Validate(); err != nil {
		return err
	}

	if err := r.BillingPeriod.Validate(); err != nil {
		return err
	}

	if err := r.BillingCycle.Validate(); err != nil {
		return err
	}

	if r.BillingPeriodCount < 1 {
		return ierr.NewError("billing_period_count must be greater than 0").
			WithHint("Billing period count must be at least 1").
			WithReportableDetails(map[string]interface{}{
				"billing_period_count": r.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.PlanID == "" {
		return ierr.NewError("plan_id is required").
			WithHint("Plan ID is required").
			Mark(ierr.ErrValidation)
	}

	if r.StartDate.After(time.Now().UTC()) {
		return ierr.NewError("start_date cannot be in the future").
			WithHint("Start date must be in the past or present").
			WithReportableDetails(map[string]interface{}{
				"start_date": r.StartDate,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.EndDate != nil && r.EndDate.Before(r.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			WithHint("End date must be after start date").
			WithReportableDetails(map[string]interface{}{
				"start_date": r.StartDate,
				"end_date":   *r.EndDate,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.TrialStart != nil && r.TrialStart.After(r.StartDate) {
		return ierr.NewError("trial_start cannot be after start_date").
			WithHint("Trial start date must be before or equal to start date").
			WithReportableDetails(map[string]interface{}{
				"start_date":  r.StartDate,
				"trial_start": *r.TrialStart,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.TrialEnd != nil && r.TrialEnd.Before(r.StartDate) {
		return ierr.NewError("trial_end cannot be before start_date").
			WithHint("Trial end date must be after or equal to start date").
			WithReportableDetails(map[string]interface{}{
				"start_date": r.StartDate,
				"trial_end":  *r.TrialEnd,
			}).
			Mark(ierr.ErrValidation)
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
		BillingCadence:     r.BillingCadence,
		BillingPeriod:      r.BillingPeriod,
		BillingPeriodCount: r.BillingPeriodCount,
		BillingAnchor:      r.StartDate,
		Metadata:           r.Metadata,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		BillingCycle:       r.BillingCycle,
	}
}

// SubscriptionLineItemRequest represents the request to create a subscription line item
type SubscriptionLineItemRequest struct {
	PriceID     string            `json:"price_id" validate:"required"`
	Quantity    decimal.Decimal   `json:"quantity" validate:"required"`
	DisplayName string            `json:"display_name,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// SubscriptionLineItemResponse represents the response for a subscription line item
type SubscriptionLineItemResponse struct {
	*subscription.SubscriptionLineItem
}

// ToSubscriptionLineItem converts a request to a domain subscription line item
func (r *SubscriptionLineItemRequest) ToSubscriptionLineItem(ctx context.Context) *subscription.SubscriptionLineItem {
	return &subscription.SubscriptionLineItem{
		ID:            types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION_LINE_ITEM),
		PriceID:       r.PriceID,
		Quantity:      r.Quantity,
		DisplayName:   r.DisplayName,
		Metadata:      r.Metadata,
		EnvironmentID: types.GetEnvironmentID(ctx),
		BaseModel:     types.GetDefaultBaseModel(ctx),
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
	MeterID          string             `json:"meter_id"`
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
