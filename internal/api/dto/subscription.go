package dto

import (
	"context"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/proration"
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
}

// UpdateSubscriptionRequest represents a request to update a subscription.
type UpdateSubscriptionRequest struct {
	// Items to be updated, added, or removed
	Items []*SubscriptionItemParam `json:"items" binding:"required,dive" validate:"required"`

	// Proration behavior determines how changes are prorated
	// Options: "create_prorations" (default), "none", "always_invoice"
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// ProrationDate is the date to use for proration calculations
	// If not provided, the current time will be used
	ProrationDate *time.Time `json:"proration_date,omitempty"`

	// ProrationStrategy determines how proration coefficients are calculated
	// Options: "day_based" (default), "second_based"
	ProrationStrategy types.ProrationStrategy `json:"proration_strategy,omitempty"`

	// Metadata is a map of key-value pairs for storing additional information
	Metadata types.Metadata `json:"metadata,omitempty"`
}

// SubscriptionItemParam represents a subscription item to be updated, added, or removed
type SubscriptionItemParam struct {
	// ID of the existing subscription item (line item)
	// If provided, the item will be updated
	// If not provided, a new item will be added
	ID string `json:"id,omitempty"`

	// PriceID is the ID of the price to use for this item
	// Required when adding a new item
	// When updating an existing item, if not provided, only the quantity will be updated
	PriceID *string `json:"price_id,omitempty"`

	// Quantity is the number of units for this item
	// Required when adding a new item
	// When updating an existing item, if not provided, the quantity will remain unchanged
	Quantity *int64 `json:"quantity,omitempty"`

	// Deleted indicates if this item should be removed from the subscription
	// Only applicable when ID is provided
	Deleted bool `json:"deleted,omitempty"`

	// Metadata is a map of key-value pairs for storing additional information
	Metadata types.Metadata `json:"metadata,omitempty"`

	// DisplayName is a user-friendly name for this item
	DisplayName *string `json:"display_name,omitempty"`
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

func (r *UpdateSubscriptionRequest) Validate() error {
	if err := validator.ValidateRequest(r); err != nil {
		return err
	}

	// Validate items
	if len(r.Items) == 0 {
		return ierr.NewError("at least one item is required").Mark(ierr.ErrValidation)
	}

	for _, item := range r.Items {
		// For new items, price_id and quantity are required
		if item.ID == "" && !item.Deleted {
			if item.PriceID == nil {
				return ierr.NewError("price_id is required for new items").
					WithHint("Price ID is required for new items").
					Mark(ierr.ErrValidation)
			}
			if item.Quantity == nil {
				return ierr.NewError("quantity is required for new items").
					WithHint("Quantity is required for new items").
					Mark(ierr.ErrValidation)
			}
		}

		// For existing items being updated, at least one of price_id or quantity must be provided
		if item.ID != "" && !item.Deleted && item.PriceID == nil && item.Quantity == nil {
			return ierr.NewError("at least one of price_id or quantity must be provided when updating an item").
				WithHint("At least one of price_id or quantity must be provided when updating an item").
				Mark(ierr.ErrValidation)
		}

		// For items being deleted, only ID and deleted=true should be provided
		if item.Deleted && item.ID == "" {
			return ierr.NewError("id is required when deleting an item").
				WithHint("Line item ID is required when deleting an item").
				Mark(ierr.ErrValidation)
		}

		// Quantity must be positive
		if item.Quantity != nil && *item.Quantity < 0 {
			return ierr.NewError("quantity must be non-negative").
				WithHint("Quantity must be non-negative").
				Mark(ierr.ErrValidation)
		}
	}

	// Validate proration behavior if provided
	if r.ProrationBehavior != "" {
		if err := r.ProrationBehavior.Validate(); err != nil {
			return err
		}
	}

	// Validate proration strategy if provided
	if r.ProrationStrategy != "" {
		if err := r.ProrationStrategy.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// CancelSubscriptionRequest represents a request to cancel a subscription
type CancelSubscriptionRequest struct {
	// CancelAtPeriodEnd determines if the subscription should be canceled at the end of the current period
	// If true, the subscription will remain active until the end of the current period
	// If false, the subscription will be canceled immediately
	CancelAtPeriodEnd bool `json:"cancel_at_period_end"`

	// ProrationOpts contains options for how to handle proration when canceling
	// If nil, default proration behavior will be used
	ProrationOpts *proration.ProrationParams `json:"proration_opts,omitempty"`
}
