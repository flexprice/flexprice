package dto

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/flexprice/flexprice/internal/domain/price"
	"github.com/flexprice/flexprice/internal/domain/subscription"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/flexprice/flexprice/internal/validator"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
)

type CreateSubscriptionRequest struct {

	// customer_id is the flexprice customer id
	// and it is prioritized over external_customer_id in case both are provided.
	CustomerID string `json:"customer_id"`
	// external_customer_id is the customer id in your DB
	// and must be same as what you provided as external_id while creating the customer in flexprice.
	ExternalCustomerID string               `json:"external_customer_id"`
	PlanID             string               `json:"plan_id" validate:"required"`
	Currency           string               `json:"currency" validate:"required,len=3"`
	LookupKey          string               `json:"lookup_key"`
	StartDate          *time.Time           `json:"start_date,omitempty"`
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
	// Credit grants to be applied when subscription is created
	CreditGrants []CreateCreditGrantRequest `json:"credit_grants,omitempty"`
	// CommitmentAmount is the minimum amount a customer commits to paying for a billing period
	CommitmentAmount *decimal.Decimal `json:"commitment_amount,omitempty"`
	// OverageFactor is a multiplier applied to usage beyond the commitment amount
	OverageFactor *decimal.Decimal `json:"overage_factor,omitempty"`
	// Phases represents an optional timeline of subscription phases
	Phases []SubscriptionSchedulePhaseInput `json:"phases,omitempty" validate:"omitempty,dive"`
	// SubscriptionCoupons is a list of coupon IDs to be applied to the subscription
	SubscriptionCoupons []string `json:"subscription_coupons,omitempty"`
	// OverrideLineItems allows customizing specific prices for this subscription
	OverrideLineItems []OverrideLineItemRequest `json:"override_line_items,omitempty" validate:"omitempty,dive"`
	// Addons represents addons to be added to the subscription during creation
	Addons []AddAddonToSubscriptionRequest `json:"addons,omitempty" validate:"omitempty,dive"`
}

// AddAddonRequest is used by body-based endpoint /subscriptions/addon
type AddAddonRequest struct {
	SubscriptionID                string `json:"subscription_id" validate:"required"`
	AddAddonToSubscriptionRequest `json:",inline"`
}

// RemoveAddonRequest is used by body-based endpoint /subscriptions/addon (DELETE)
type RemoveAddonRequest struct {
	SubscriptionID string `json:"subscription_id" validate:"required"`
	AddonID        string `json:"addon_id" validate:"required"`
	Reason         string `json:"reason"`
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
	// Schedule is included when the subscription has a schedule
	Schedule *SubscriptionScheduleResponse `json:"schedule,omitempty"`
	// CouponAssociations are the coupon associations for this subscription
	CouponAssociations []*CouponAssociationResponse `json:"coupon_associations,omitempty"`
}

// ListSubscriptionsResponse represents the response for listing subscriptions
type ListSubscriptionsResponse = types.ListResponse[*SubscriptionResponse]

func (r *CreateSubscriptionRequest) Validate() error {
	// Case- Both are absent
	if r.CustomerID == "" && r.ExternalCustomerID == "" {
		return ierr.NewError("either customer_id or external_customer_id is required").
			WithHint("Please provide either customer_id or external_customer_id").
			Mark(ierr.ErrValidation)
	}

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

	// Set default start date if not provided
	if r.StartDate == nil {
		now := time.Now().UTC()
		r.StartDate = &now
	}

	if r.EndDate != nil && r.EndDate.Before(*r.StartDate) {
		return ierr.NewError("end_date cannot be before start_date").
			WithHint("End date must be after start date").
			WithReportableDetails(map[string]interface{}{
				"start_date": *r.StartDate,
				"end_date":   *r.EndDate,
			}).
			Mark(ierr.ErrValidation)
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

	if r.StartDate != nil && r.StartDate.After(time.Now().UTC()) {
		return ierr.NewError("start_date cannot be in the future").
			WithHint("Start date must be in the past or present").
			WithReportableDetails(map[string]interface{}{
				"start_date": *r.StartDate,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.TrialStart != nil && r.TrialStart.After(*r.StartDate) {
		return ierr.NewError("trial_start cannot be after start_date").
			WithHint("Trial start date must be before or equal to start date").
			WithReportableDetails(map[string]interface{}{
				"start_date":  *r.StartDate,
				"trial_start": *r.TrialStart,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.TrialEnd != nil && r.TrialEnd.Before(*r.StartDate) {
		return ierr.NewError("trial_end cannot be before start_date").
			WithHint("Trial end date must be after or equal to start date").
			WithReportableDetails(map[string]interface{}{
				"start_date": *r.StartDate,
				"trial_end":  *r.TrialEnd,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate commitment amount and overage factor
	if r.CommitmentAmount != nil && r.CommitmentAmount.LessThan(decimal.Zero) {
		return ierr.NewError("commitment_amount must be non-negative").
			WithHint("Commitment amount must be greater than or equal to 0").
			WithReportableDetails(map[string]interface{}{
				"commitment_amount": *r.CommitmentAmount,
			}).
			Mark(ierr.ErrValidation)
	}

	if r.OverageFactor != nil && r.OverageFactor.LessThan(decimal.NewFromInt(1)) {
		return ierr.NewError("overage_factor must be at least 1.0").
			WithHint("Overage factor must be greater than or equal to 1.0").
			WithReportableDetails(map[string]interface{}{
				"overage_factor": *r.OverageFactor,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate credit grants if provided
	if len(r.CreditGrants) > 0 {
		for i, grant := range r.CreditGrants {

			// Force scope to SUBSCRIPTION for all grants added this way
			if grant.Scope != types.CreditGrantScopeSubscription {
				return ierr.NewError("invalid credit grant scope").
					WithHint("Credit grants created with a subscription must have SUBSCRIPTION scope").
					WithReportableDetails(map[string]interface{}{
						"grant_scope": grant.Scope,
						"grant_index": i,
					}).
					Mark(ierr.ErrValidation)
			}

			if err := grant.Validate(); err != nil {
				return err
			}
		}
	}

	// Validate phases if provided
	if len(r.Phases) > 0 {
		// First phase must start on or after subscription start date
		if r.Phases[0].StartDate.Before(*r.StartDate) {
			return ierr.NewError("first phase start date cannot be before subscription start date").
				WithHint("The first phase must start on or after the subscription start date").
				WithReportableDetails(map[string]interface{}{
					"subscription_start_date": *r.StartDate,
					"first_phase_start_date":  r.Phases[0].StartDate,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate each phase
		for i, phase := range r.Phases {
			// Validate the phase itself
			if err := phase.Validate(); err != nil {
				return ierr.NewError(fmt.Sprintf("invalid phase at index %d", i)).
					WithHint("Phase validation failed").
					WithReportableDetails(map[string]interface{}{
						"index": i,
						"error": err.Error(),
					}).
					Mark(ierr.ErrValidation)
			}

			// Validate phase continuity
			if i > 0 {
				prevPhase := r.Phases[i-1]
				if prevPhase.EndDate == nil {
					return ierr.NewError(fmt.Sprintf("phase at index %d must have an end date", i-1)).
						WithHint("All phases except the last one must have an end date").
						Mark(ierr.ErrValidation)
				}

				if !prevPhase.EndDate.Equal(phase.StartDate) {
					return ierr.NewError(fmt.Sprintf("phase at index %d does not start immediately after previous phase", i)).
						WithHint("Phases must be contiguous").
						WithReportableDetails(map[string]interface{}{
							"previous_phase_end":  prevPhase.EndDate,
							"current_phase_start": phase.StartDate,
						}).
						Mark(ierr.ErrValidation)
				}
			}
		}
	}

	// Validate subscription coupons if provided
	if len(r.SubscriptionCoupons) > 0 {
		// Validate that coupon IDs are not empty
		for i, couponID := range r.SubscriptionCoupons {
			if couponID == "" {
				return ierr.NewError("subscription coupon ID cannot be empty").
					WithHint("All subscription coupon IDs must be valid").
					WithReportableDetails(map[string]interface{}{
						"index": i,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Validate override line items if provided
	if len(r.OverrideLineItems) > 0 {
		priceIDsSeen := make(map[string]bool)
		for i, override := range r.OverrideLineItems {
			if err := override.Validate(); err != nil {
				return ierr.NewError(fmt.Sprintf("invalid override line item at index %d", i)).
					WithHint("Override line item validation failed").
					WithReportableDetails(map[string]interface{}{
						"index": i,
						"error": err.Error(),
					}).
					Mark(ierr.ErrValidation)
			}

			// Check for duplicate price IDs
			if priceIDsSeen[override.PriceID] {
				return ierr.NewError(fmt.Sprintf("duplicate price_id in override line items at index %d", i)).
					WithHint("Each price can only be overridden once per subscription").
					WithReportableDetails(map[string]interface{}{
						"price_id": override.PriceID,
						"index":    i,
					}).
					Mark(ierr.ErrValidation)
			}
			priceIDsSeen[override.PriceID] = true
		}
	}

	return nil
}

func (r *CreateSubscriptionRequest) ToSubscription(ctx context.Context) *subscription.Subscription {

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         r.CustomerID,
		PlanID:             r.PlanID,
		Currency:           strings.ToLower(r.Currency),
		LookupKey:          r.LookupKey,
		SubscriptionStatus: types.SubscriptionStatusActive,
		StartDate:          *r.StartDate,
		EndDate:            r.EndDate,
		TrialStart:         r.TrialStart,
		TrialEnd:           r.TrialEnd,
		BillingCadence:     r.BillingCadence,
		BillingPeriod:      r.BillingPeriod,
		BillingPeriodCount: r.BillingPeriodCount,
		BillingAnchor:      *r.StartDate,
		Metadata:           r.Metadata,
		EnvironmentID:      types.GetEnvironmentID(ctx),
		BaseModel:          types.GetDefaultBaseModel(ctx),
		BillingCycle:       r.BillingCycle,
	}

	// Set commitment amount and overage factor if provided
	if r.CommitmentAmount != nil {
		sub.CommitmentAmount = r.CommitmentAmount
	}

	if r.OverageFactor != nil {
		sub.OverageFactor = r.OverageFactor
	} else {
		sub.OverageFactor = lo.ToPtr(decimal.NewFromInt(1)) // Default value
	}

	return sub
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

// OverrideLineItemRequest represents a price override for a specific subscription
type OverrideLineItemRequest struct {
	// PriceID references the plan price to override
	PriceID string `json:"price_id" validate:"required"`
	// Quantity for this line item (optional)
	Quantity *decimal.Decimal `json:"quantity,omitempty"`
	// Amount is the new price amount that overrides the original price (optional)
	Amount *decimal.Decimal `json:"amount,omitempty"`
}

// Validate validates the override line item request
func (r *OverrideLineItemRequest) Validate() error {
	if r.PriceID == "" {
		return ierr.NewError("price_id is required for override line items").
			WithHint("Price ID must be specified for price overrides").
			Mark(ierr.ErrValidation)
	}

	// At least one override field (quantity or amount) must be provided
	if r.Quantity == nil && r.Amount == nil {
		return ierr.NewError("at least one override field (quantity or amount) must be provided").
			WithHint("Specify either quantity, amount, or both for price override").
			Mark(ierr.ErrValidation)
	}

	// Validate amount if provided
	if r.Amount != nil && r.Amount.IsNegative() {
		return ierr.NewError("amount must be non-negative").
			WithHint("Override amount cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"amount": r.Amount.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate quantity if provided
	if r.Quantity != nil && r.Quantity.IsNegative() {
		return ierr.NewError("quantity must be non-negative").
			WithHint("Override quantity cannot be negative").
			WithReportableDetails(map[string]interface{}{
				"quantity": r.Quantity.String(),
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
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
	Amount             float64                              `json:"amount"`
	Currency           string                               `json:"currency"`
	DisplayAmount      string                               `json:"display_amount"`
	StartTime          time.Time                            `json:"start_time"`
	EndTime            time.Time                            `json:"end_time"`
	Charges            []*SubscriptionUsageByMetersResponse `json:"charges"`
	CommitmentAmount   float64                              `json:"commitment_amount,omitempty"`
	OverageFactor      float64                              `json:"overage_factor,omitempty"`
	CommitmentUtilized float64                              `json:"commitment_utilized,omitempty"` // Amount of commitment used
	OverageAmount      float64                              `json:"overage_amount,omitempty"`      // Amount charged at overage rate
	HasOverage         bool                                 `json:"has_overage"`                   // Whether any usage exceeded commitment
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
	IsOverage        bool               `json:"is_overage"`               // Whether this charge is at overage rate
	OverageFactor    float64            `json:"overage_factor,omitempty"` // Factor applied to this charge if in overage
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
