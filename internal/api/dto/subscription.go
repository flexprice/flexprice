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
	BillingPeriodCount int                  `json:"billing_period_count" default:"1"`
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
	// tax_rate_overrides is the tax rate overrides	to be applied to the subscription
	TaxRateOverrides []*TaxRateOverride `json:"tax_rate_overrides,omitempty"`
	// SubscriptionCoupons is a list of coupon IDs to be applied to the subscription
	Coupons []string `json:"coupons,omitempty"`
	// SubscriptionLineItemsCoupons is a list of coupon IDs to be applied to the subscription line items
	LineItemCoupons map[string][]string `json:"line_item_coupons,omitempty"`
	// OverrideLineItems allows customizing specific prices for this subscription
	OverrideLineItems []OverrideLineItemRequest `json:"override_line_items,omitempty" validate:"omitempty,dive"`
	// OverrideEntitlements allows customizing specific entitlements for this subscription
	OverrideEntitlements []OverrideEntitlementRequest `json:"override_entitlements,omitempty" validate:"omitempty,dive"`
	// Addons represents addons to be added to the subscription during creation
	Addons []AddAddonToSubscriptionRequest `json:"addons,omitempty" validate:"omitempty,dive"`

	// Payment behavior configuration
	PaymentBehavior        *types.PaymentBehavior `json:"payment_behavior,omitempty"`
	GatewayPaymentMethodID *string                `json:"gateway_payment_method_id,omitempty"`
	// collection_method determines how invoices are collected
	// "default_incomplete" - subscription waits for payment confirmation before activation
	// "send_invoice" - subscription activates immediately, invoice is sent for payment
	CollectionMethod *types.CollectionMethod `json:"collection_method,omitempty"`

	// ProrationBehavior controls how proration is handled.
	// If not set, the default value is none. Possible values are create_prorations and none.
	// create_prorations means the proration will be calculated and applied.
	// none means the proration will not be calculated.
	// This is IGNORED when the billing cycle is anniversary.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// Timezone of the customer.
	// If not set, the default value is UTC.
	CustomerTimezone string `json:"customer_timezone" validate:"omitempty,timezone"`

	//Billing Anchor
	BillingAnchor *time.Time `json:"-"`

	// Workflow
	Workflow           *types.TemporalWorkflowType `json:"-"`
	SubscriptionStatus types.SubscriptionStatus    `json:"-,omitempty"`
}

// AddAddonRequest is used by body-based endpoint /subscriptions/addon
type AddAddonRequest struct {
	SubscriptionID                string `json:"subscription_id" validate:"required"`
	AddAddonToSubscriptionRequest `json:",inline"`
}

// RemoveAddonRequest is used by body-based endpoint /subscriptions/addon (DELETE)
type RemoveAddonRequest struct {
	AddonAssociationID string     `json:"addon_association_id" validate:"required"`
	Reason             string     `json:"reason"`
	EffectiveFrom      *time.Time `json:"effective_from,omitempty"`
}

func (r *RemoveAddonRequest) Validate() error {

	if r.EffectiveFrom != nil && r.EffectiveFrom.Before(time.Now()) {
		return ierr.NewError("effective_from date is invalid").
			WithHint("end date cannot be in the past").
			WithReportableDetails(map[string]interface{}{
				"addon_association_id": r.AddonAssociationID,
				"effective_from":       r.EffectiveFrom,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

type UpdateSubscriptionRequest struct {
	Status            types.SubscriptionStatus `json:"status"`
	CancelAt          *time.Time               `json:"cancel_at,omitempty"`
	CancelAtPeriodEnd bool                     `json:"cancel_at_period_end,omitempty"`
}

// CancelSubscriptionRequest represents the enhanced cancellation request
type CancelSubscriptionRequest struct {

	// ProrationBehavior controls how proration is handled.
	ProrationBehavior types.ProrationBehavior `json:"proration_behavior,omitempty"`

	// CancellationType determines when the cancellation takes effect
	CancellationType types.CancellationType `json:"cancellation_type" validate:"required"`

	// Reason for cancellation (for audit and business intelligence)
	Reason string `json:"reason,omitempty"`

	//SuppressWebhook is an internal flag to suppress webhook events during cancellation.
	SuppressWebhook bool `json:"_,omitempty"`
}

// CancelSubscriptionResponse represents the enhanced cancellation response
type CancelSubscriptionResponse struct {
	// Basic cancellation info
	SubscriptionID   string                   `json:"subscription_id"`
	CancellationType types.CancellationType   `json:"cancellation_type"`
	EffectiveDate    time.Time                `json:"effective_date"`
	Status           types.SubscriptionStatus `json:"status"`
	Reason           string                   `json:"reason,omitempty"`

	// Proration details
	ProrationInvoice  *InvoiceResponse  `json:"proration_invoice,omitempty"`
	ProrationDetails  []ProrationDetail `json:"proration_details"`
	TotalCreditAmount decimal.Decimal   `json:"total_credit_amount"`

	// Response metadata
	Message     string    `json:"message"`
	ProcessedAt time.Time `json:"processed_at"`
}

// ProrationDetail provides line-item level proration information
type ProrationDetail struct {
	LineItemID     string          `json:"line_item_id"`
	PriceID        string          `json:"price_id"`
	PlanName       string          `json:"plan_name,omitempty"`
	OriginalAmount decimal.Decimal `json:"original_amount"`
	CreditAmount   decimal.Decimal `json:"credit_amount"`
	ChargeAmount   decimal.Decimal `json:"charge_amount"`
	ProrationDays  int             `json:"proration_days"`
	Description    string          `json:"description,omitempty"`
}

// Validate validates the cancellation request
func (r *CancelSubscriptionRequest) Validate() error {
	// Validate cancellation type
	if err := r.CancellationType.Validate(); err != nil {
		return err
	}
	// Set default proration behavior if not provided
	if r.ProrationBehavior == "" {
		r.ProrationBehavior = types.ProrationBehaviorNone
	}

	return nil
}

type SubscriptionResponse struct {
	*subscription.Subscription
	Plan     *PlanResponse     `json:"plan"`
	Customer *CustomerResponse `json:"customer"`
	// Schedule is included when the subscription has a schedule
	Schedule *SubscriptionScheduleResponse `json:"schedule,omitempty"`
	// CouponAssociations are the coupon associations for this subscription
	CouponAssociations []*CouponAssociationResponse `json:"coupon_associations,omitempty"`

	// Latest invoice information for incomplete subscriptions
	LatestInvoice *InvoiceResponse `json:"latest_invoice,omitempty"`
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

	// Handle legacy collection method conversion and validation
	if r.CollectionMethod != nil {
		// Handle legacy default_incomplete collection method
		if string(*r.CollectionMethod) == "default_incomplete" {
			// Convert to charge_automatically + allow_incomplete for backward compatibility
			chargeAutomaticallyMethod := types.CollectionMethodChargeAutomatically
			r.CollectionMethod = &chargeAutomaticallyMethod
			if r.PaymentBehavior == nil || *r.PaymentBehavior == types.PaymentBehaviorDefaultIncomplete {
				allowIncomplete := types.PaymentBehaviorAllowIncomplete
				r.PaymentBehavior = &allowIncomplete
			}
		}

		if err := r.CollectionMethod.Validate(); err != nil {
			return err
		}
	}

	// Set defaults for collection method and payment behavior if not provided
	if r.CollectionMethod == nil {
		defaultCollectionMethod := types.CollectionMethodChargeAutomatically
		r.CollectionMethod = &defaultCollectionMethod
	}
	if r.PaymentBehavior == nil {
		defaultPaymentBehavior := types.PaymentBehaviorDefaultActive
		r.PaymentBehavior = &defaultPaymentBehavior
	}

	// Set default value to Billing Period Count if not provided
	if r.BillingPeriodCount == 0 {
		r.BillingPeriodCount = 1
	} else if r.BillingPeriodCount < 0 {
		return ierr.NewError("invalid billing period count").
			WithHint("Billing Period must be a valid positive number").
			WithReportableDetails(map[string]interface{}{
				"billing_period_count": r.BillingPeriodCount,
			}).
			Mark(ierr.ErrValidation)
	}

	// Validate payment behavior and collection method combination
	if err := r.validatePaymentBehaviorForCollectionMethod(*r.CollectionMethod, *r.PaymentBehavior); err != nil {
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

	if r.ProrationBehavior != "" {
		if err := r.ProrationBehavior.Validate(); err != nil {
			return err
		}
	} else {
		r.ProrationBehavior = types.ProrationBehaviorNone
	}
	if r.Workflow == nil {
		r.Workflow = lo.ToPtr(types.TemporalSubscriptionChangeWorkflow)
	}

	if r.ProrationBehavior != "" && r.ProrationBehavior == types.ProrationBehaviorCreateProrations {
		if err := r.validateShouldAllowProrationOnStartDate(r); err != nil {
			return err
		}
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

	// taxrate overrides validation
	if len(r.TaxRateOverrides) > 0 {
		for _, taxRateOverride := range r.TaxRateOverrides {
			if err := taxRateOverride.Validate(); err != nil {
				return ierr.NewError("invalid tax rate override").
					WithHint("Tax rate override validation failed").
					WithReportableDetails(map[string]interface{}{
						"error":             err.Error(),
						"tax_rate_override": taxRateOverride,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Validate subscription coupons if provided
	if len(r.Coupons) > 0 {
		// Validate that coupon IDs are not empty
		for i, couponID := range r.Coupons {
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

	if len(r.LineItemCoupons) > 0 {
		for priceID, couponIDs := range r.LineItemCoupons {
			if len(couponIDs) == 0 {
				return ierr.NewError("subscription line item coupon IDs cannot be empty").
					WithHint("All subscription line item coupon IDs must be valid").
					WithReportableDetails(map[string]interface{}{
						"price_id": priceID,
					}).
					Mark(ierr.ErrValidation)
			}
		}
	}
	// Validate override line items if provided
	if len(r.OverrideLineItems) > 0 {
		priceIDsSeen := make(map[string]bool)
		for i, override := range r.OverrideLineItems {
			if err := override.Validate(nil, nil, r.PlanID); err != nil {
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

// validatePaymentBehaviorForCollectionMethod validates that payment behavior is compatible with collection method
func (r *CreateSubscriptionRequest) validatePaymentBehaviorForCollectionMethod(collectionMethod types.CollectionMethod, paymentBehavior types.PaymentBehavior) error {
	switch collectionMethod {
	case types.CollectionMethodChargeAutomatically:
		// For charge_automatically, allow_incomplete, error_if_incomplete, and default_active are allowed
		if paymentBehavior != types.PaymentBehaviorAllowIncomplete &&
			paymentBehavior != types.PaymentBehaviorErrorIfIncomplete &&
			paymentBehavior != types.PaymentBehaviorDefaultActive {
			return ierr.NewError("invalid payment behavior for charge_automatically collection method").
				WithHint("Only allow_incomplete, error_if_incomplete, and default_active are supported for charge_automatically collection method").
				WithReportableDetails(map[string]interface{}{
					"collection_method": collectionMethod,
					"payment_behavior":  paymentBehavior,
					"allowed_behaviors": []types.PaymentBehavior{
						types.PaymentBehaviorAllowIncomplete,
						types.PaymentBehaviorErrorIfIncomplete,
						types.PaymentBehaviorDefaultActive,
					},
				}).
				Mark(ierr.ErrValidation)
		}
	case types.CollectionMethodSendInvoice:
		// For send_invoice, only default_active and default_incomplete are allowed
		if paymentBehavior != types.PaymentBehaviorDefaultActive && paymentBehavior != types.PaymentBehaviorDefaultIncomplete {
			return ierr.NewError("invalid payment behavior for send_invoice collection method").
				WithHint("Only default_active and default_incomplete are supported for send_invoice collection method").
				WithReportableDetails(map[string]interface{}{
					"collection_method": collectionMethod,
					"payment_behavior":  paymentBehavior,
					"allowed_behaviors": []types.PaymentBehavior{
						types.PaymentBehaviorDefaultActive,
						types.PaymentBehaviorDefaultIncomplete,
					},
				}).
				Mark(ierr.ErrValidation)
		}

	default:
		return ierr.NewError("unsupported collection method").
			WithHint("Only charge_automatically and send_invoice collection methods are supported").
			WithReportableDetails(map[string]interface{}{
				"collection_method": collectionMethod,
			}).
			Mark(ierr.ErrValidation)
	}

	return nil
}

func (r *CreateSubscriptionRequest) validateShouldAllowProrationOnStartDate(request *CreateSubscriptionRequest) error {
	// If the start date is before the current date and proration mode is active, return an error
	// This prevents creating subscriptions with backdated start dates that would trigger proration

	if request.Workflow == lo.ToPtr(types.TemporalSubscriptionCreationWorkflow) {
		return nil
	}

	// Compare only the date portions (ignore time) - don't allow before current day
	now := time.Now().UTC()
	startDateOnly := time.Date(request.StartDate.Year(), request.StartDate.Month(), request.StartDate.Day(), 0, 0, 0, 0, time.UTC)
	nowDateOnly := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	if startDateOnly.Before(nowDateOnly) {
		return ierr.NewError("cannot create subscription with past start date when proration is active and workflow is subscription change").
			WithHint("Either set start date to current day or later, or disable proration behavior").
			WithReportableDetails(map[string]interface{}{
				"start_date":   request.StartDate.Format("2006-01-02"),
				"current_date": nowDateOnly.Format("2006-01-02"),
			}).
			Mark(ierr.ErrValidation)
	}
	return nil
}

func (r *CreateSubscriptionRequest) ToSubscription(ctx context.Context) *subscription.Subscription {
	// Handle legacy collection method and set defaults
	paymentBehavior := types.PaymentBehaviorDefaultActive
	collectionMethod := types.CollectionMethodChargeAutomatically

	// Handle legacy default_incomplete collection method conversion
	if r.CollectionMethod != nil && string(*r.CollectionMethod) == "default_incomplete" {
		// Convert legacy default_incomplete collection method to charge_automatically + allow_incomplete
		collectionMethod = types.CollectionMethodChargeAutomatically
		paymentBehavior = types.PaymentBehaviorAllowIncomplete
	} else {
		// Normal flow - use provided values or defaults
		if r.CollectionMethod != nil {
			collectionMethod = *r.CollectionMethod
		}
		if r.PaymentBehavior != nil {
			paymentBehavior = *r.PaymentBehavior
		}
	}

	// Set initial status based on payment behavior
	var initialStatus types.SubscriptionStatus
	switch paymentBehavior {
	case types.PaymentBehaviorDefaultActive:
		// Default active behavior - subscription starts as active
		initialStatus = types.SubscriptionStatusActive
	case types.PaymentBehaviorDefaultIncomplete:
		// Default incomplete behavior - subscription starts as incomplete
		initialStatus = types.SubscriptionStatusIncomplete
	case types.PaymentBehaviorAllowIncomplete:
		// Allow incomplete behavior - subscription starts as incomplete (will be updated based on payment result)
		initialStatus = types.SubscriptionStatusIncomplete
	case types.PaymentBehaviorErrorIfIncomplete:
		// Error if incomplete behavior - subscription starts as incomplete (will fail if payment fails)
		initialStatus = types.SubscriptionStatusIncomplete
	default:
		// Fallback to active for unknown behaviors
		initialStatus = types.SubscriptionStatusActive
	}

	if r.CustomerTimezone == "" {
		r.CustomerTimezone = "UTC"
	}

	sub := &subscription.Subscription{
		ID:                 types.GenerateUUIDWithPrefix(types.UUID_PREFIX_SUBSCRIPTION),
		CustomerID:         r.CustomerID,
		PlanID:             r.PlanID,
		Currency:           strings.ToLower(r.Currency),
		LookupKey:          r.LookupKey,
		SubscriptionStatus: initialStatus,
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
		CustomerTimezone:   r.CustomerTimezone,
		ProrationBehavior:  r.ProrationBehavior,

		// New payment behavior fields
		PaymentBehavior:        string(paymentBehavior),
		CollectionMethod:       string(collectionMethod),
		GatewayPaymentMethodID: r.GatewayPaymentMethodID,
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
	Price *PriceResponse `json:"price,omitempty"`
}

// OverrideLineItemRequest represents a price override for a specific subscription
type OverrideLineItemRequest struct {
	// PriceID references the plan price to override
	PriceID string `json:"price_id" validate:"required"`

	// Quantity for this line item (optional)
	Quantity *decimal.Decimal `json:"quantity,omitempty"`

	BillingModel types.BillingModel `json:"billing_model,omitempty"`

	// Amount is the new price amount that overrides the original price (optional)
	Amount *decimal.Decimal `json:"amount,omitempty"`

	// TierMode determines how to calculate the price for a given quantity
	TierMode types.BillingTier `json:"tier_mode,omitempty"`

	// Tiers determines the pricing tiers for this line item
	Tiers []CreatePriceTier `json:"tiers,omitempty"`

	// TransformQuantity determines how to transform the quantity for this line item
	TransformQuantity *price.TransformQuantity `json:"transform_quantity,omitempty"`
}

// OverrideEntitlementRequest allows overriding entitlement values for a subscription
type OverrideEntitlementRequest struct {
	// EntitlementID references the plan/addon entitlement to override
	EntitlementID string `json:"entitlement_id" validate:"required"`

	// UsageLimit is the new usage limit (only these 3 fields can be overridden)
	// For metered features, nil means unlimited usage
	UsageLimit *int64 `json:"usage_limit"`

	// IsEnabled determines if the entitlement is enabled or disabled
	IsEnabled *bool `json:"is_enabled,omitempty"`

	// StaticValue is the static value for static features
	StaticValue *string `json:"static_value,omitempty"`
}

// Validate validates the entitlement override request
func (r *OverrideEntitlementRequest) Validate() error {
	if r.EntitlementID == "" {
		return ierr.NewError("entitlement_id is required").
			WithHint("Please provide the entitlement ID to override").
			Mark(ierr.ErrValidation)
	}

	return nil
}

// Validate validates the override line item request with additional context
// This method should be called after basic validation to check business rules
func (r *OverrideLineItemRequest) Validate(
	priceMap map[string]*PriceResponse,
	lineItemsByPriceID map[string]*subscription.SubscriptionLineItem,
	EntityId string,
) error {
	if r.PriceID == "" {
		return ierr.NewError("price_id is required for override line items").
			WithHint("Price ID must be specified for price overrides").
			Mark(ierr.ErrValidation)
	}

	// At least one override field (quantity, amount, billing_model, tier_mode, tiers, or transform_quantity) must be provided
	if r.Quantity == nil && r.Amount == nil && r.BillingModel == "" && r.TierMode == "" && len(r.Tiers) == 0 && r.TransformQuantity == nil {
		return ierr.NewError("at least one override field must be provided").
			WithHint("Specify at least one of: quantity, amount, billing_model, tier_mode, tiers, or transform_quantity for price override").
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

	// Validate billing model if provided
	if r.BillingModel != "" {
		if err := r.BillingModel.Validate(); err != nil {
			return err
		}

		// Billing model specific validations
		switch r.BillingModel {
		case types.BILLING_MODEL_TIERED:
			// Check for tiers in either tier_mode or tiers
			hasTierMode := r.TierMode != ""
			hasTiers := len(r.Tiers) > 0

			if !hasTierMode && !hasTiers {
				return ierr.NewError("tier_mode or tiers are required when billing model is TIERED").
					WithHint("Please provide either tier_mode or tiers for tiered pricing override").
					Mark(ierr.ErrValidation)
			}

			// Validate tier mode if provided
			if r.TierMode != "" {
				if err := r.TierMode.Validate(); err != nil {
					return err
				}
			}

			// Validate tiers if provided
			if len(r.Tiers) > 0 {
				for i, tier := range r.Tiers {
					if tier.UnitAmount == "" {
						return ierr.NewError("unit_amount is required when tiers are provided").
							WithHint("Please provide a valid unit amount for each tier").
							WithReportableDetails(map[string]interface{}{
								"tier_index": i,
							}).
							Mark(ierr.ErrValidation)
					}

					// Validate tier unit amount is a valid decimal
					tierUnitAmount, err := decimal.NewFromString(tier.UnitAmount)
					if err != nil {
						return ierr.NewError("invalid tier unit amount format").
							WithHint("Tier unit amount must be a valid decimal number").
							WithReportableDetails(map[string]interface{}{
								"tier_index":  i,
								"unit_amount": tier.UnitAmount,
							}).
							Mark(ierr.ErrValidation)
					}

					// Validate tier unit amount is not negative (allows zero)
					if tierUnitAmount.IsNegative() {
						return ierr.NewError("tier unit amount cannot be negative").
							WithHint("Tier unit amount cannot be negative").
							WithReportableDetails(map[string]interface{}{
								"tier_index":  i,
								"unit_amount": tier.UnitAmount,
							}).
							Mark(ierr.ErrValidation)
					}

					// Validate flat amount if provided
					if tier.FlatAmount != nil {
						flatAmount, err := decimal.NewFromString(*tier.FlatAmount)
						if err != nil {
							return ierr.NewError("invalid tier flat amount format").
								WithHint("Tier flat amount must be a valid decimal number").
								WithReportableDetails(map[string]interface{}{
									"tier_index":  i,
									"flat_amount": tier.FlatAmount,
								}).
								Mark(ierr.ErrValidation)
						}

						if flatAmount.IsNegative() {
							return ierr.NewError("tier flat amount cannot be negative").
								WithHint("Tier flat amount cannot be negative").
								WithReportableDetails(map[string]interface{}{
									"tier_index":  i,
									"flat_amount": tier.FlatAmount,
								}).
								Mark(ierr.ErrValidation)
						}
					}
				}
			}

		case types.BILLING_MODEL_PACKAGE:
			if r.TransformQuantity == nil {
				return ierr.NewError("transform_quantity is required when billing model is PACKAGE").
					WithHint("Please provide the number of units to set up package pricing override").
					Mark(ierr.ErrValidation)
			}

			if r.TransformQuantity.DivideBy <= 0 {
				return ierr.NewError("transform_quantity.divide_by must be greater than 0 when billing model is PACKAGE").
					WithHint("Please provide a valid number of units to set up package pricing override").
					Mark(ierr.ErrValidation)
			}

			// Validate round type
			if r.TransformQuantity.Round == "" {
				r.TransformQuantity.Round = types.ROUND_UP // Default to rounding up
			} else if r.TransformQuantity.Round != types.ROUND_UP && r.TransformQuantity.Round != types.ROUND_DOWN {
				return ierr.NewError("invalid rounding type- allowed values are up and down").
					WithHint("Please provide a valid rounding type for package pricing override").
					WithReportableDetails(map[string]interface{}{
						"round":   r.TransformQuantity.Round,
						"allowed": []string{types.ROUND_UP, types.ROUND_DOWN},
					}).
					Mark(ierr.ErrValidation)
			}

		case types.BILLING_MODEL_FLAT_FEE:
			// For flat fee, amount is typically required unless quantity is being overridden
			if r.Amount == nil && r.Quantity == nil {
				return ierr.NewError("amount or quantity is required when billing model is FLAT_FEE").
					WithHint("Please provide either amount or quantity for flat fee pricing override").
					Mark(ierr.ErrValidation)
			}
		}
	}

	// Validate tier mode if provided (independent of billing model)
	if r.TierMode != "" {
		if err := r.TierMode.Validate(); err != nil {
			return err
		}
	}

	// Validate transform quantity if provided (independent of billing model)
	if r.TransformQuantity != nil {
		if r.TransformQuantity.DivideBy <= 0 {
			return ierr.NewError("transform_quantity.divide_by must be greater than 0").
				WithHint("Transform quantity divide_by must be greater than 0").
				Mark(ierr.ErrValidation)
		}

		if r.TransformQuantity.Round != "" && r.TransformQuantity.Round != types.ROUND_UP && r.TransformQuantity.Round != types.ROUND_DOWN {
			return ierr.NewError("invalid rounding type- allowed values are up and down").
				WithHint("Please provide a valid rounding type").
				WithReportableDetails(map[string]interface{}{
					"round":   r.TransformQuantity.Round,
					"allowed": []string{types.ROUND_UP, types.ROUND_DOWN},
				}).
				Mark(ierr.ErrValidation)
		}
	}

	// If context is provided, do additional validation
	if priceMap != nil && lineItemsByPriceID != nil && EntityId != "" {
		// Validate that the price exists in the plan
		_, exists := priceMap[r.PriceID]
		if !exists {
			return ierr.NewError("price not found in plan").
				WithHint("Override price must be a valid price from the selected plan").
				WithReportableDetails(map[string]interface{}{
					"price_id": r.PriceID,
					"plan_id":  EntityId,
				}).
				Mark(ierr.ErrValidation)
		}

		// Validate that the line item exists for this price
		_, exists = lineItemsByPriceID[r.PriceID]
		if !exists {
			return ierr.NewError("line item not found for price").
				WithHint("Could not find line item for the specified price").
				WithReportableDetails(map[string]interface{}{
					"price_id": r.PriceID,
				}).
				Mark(ierr.ErrInternal)
		}
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
